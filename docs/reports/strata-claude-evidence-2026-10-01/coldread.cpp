// (1) sequential O_DIRECT read, 8 MiB requests, 1 GiB, 1 and 4 threads (startup / complement-prefetch bound);
// (2) cold mmap touch of 64 random expert blobs (what a CPU-pool miss or Stager copy does on a cold page cache).
#include <fcntl.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <random>
#include <thread>
#include <vector>
int main(int argc, char** argv) {
    const char* path = argv[1];
    const long long blob = atoll(argv[2]), base_off = atoll(argv[3]);
    struct stat st; stat(path, &st);
    for (int threads : {1, 4}) {
        const long long total = 1ll << 30, req = 8ll << 20, start = base_off + (threads == 1 ? 0 : (2ll << 30));
        auto t0 = std::chrono::steady_clock::now();
        std::vector<std::thread> ts;
        for (int t = 0; t < threads; ++t)
            ts.emplace_back([&, t] {
                int fd = open(path, O_RDONLY | O_DIRECT);
                void* b = nullptr; posix_memalign(&b, 4096, req);
                for (long long o = t * req; o < total; o += threads * req)
                    if (pread(fd, b, req, start + o) != req) { perror("pread"); std::exit(1); }
                free(b); close(fd);
            });
        for (auto& x : ts) x.join();
        double s = std::chrono::duration<double>(std::chrono::steady_clock::now() - t0).count();
        std::printf("sequential O_DIRECT 8 MiB x %d thread(s): %.2f GB/s\n", threads, total / s / 1e9);
    }
    int fd = open(path, O_RDONLY);
    const unsigned char* m = (const unsigned char*) mmap(nullptr, st.st_size, PROT_READ, MAP_SHARED, fd, 0);
    std::mt19937_64 rng(99);
    const long long nblobs = (st.st_size - (8ll << 30)) / blob;
    std::vector<double> ms;
    volatile unsigned long long sink = 0;
    for (int i = 0; i < 64; ++i) {
        const long long b = (long long) (rng() % nblobs) + (8ll << 30) / blob;   // past the region read above
        const unsigned char* p = m + b * blob;
        auto a = std::chrono::steady_clock::now();
        unsigned long long s = 0;
        for (long long k = 0; k < blob; k += 4096) s += p[k];
        sink += s;
        ms.push_back(std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - a).count());
    }
    std::sort(ms.begin(), ms.end());
    double sum = 0; for (double x : ms) sum += x;
    std::printf("cold mmap touch of 64 random %.2f MB blobs: mean %.2f ms/blob (%.2f GB/s), p50 %.2f ms, max %.2f ms\n",
                blob / 1e6, sum / ms.size(), blob / (sum / ms.size() / 1e3) / 1e9, ms[32], ms.back());
}
