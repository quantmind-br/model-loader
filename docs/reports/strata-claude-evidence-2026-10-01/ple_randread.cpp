// Random 4 KiB O_DIRECT reads inside the PLE table (per_layer_token_embd: 320,001,536 rows x 90 B) with N blocking
// threads, as src/platform/direct_file.cpp does (thread count = queue depth). Read-only.
#include <fcntl.h>
#include <unistd.h>
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <random>
#include <thread>
#include <vector>
int main(int argc, char** argv) {
    const char* path = argv[1];
    const long long data_start = atoll(argv[2]);
    const long long table = 28800138240LL;
    const int threads = atoi(argv[3]), per = atoi(argv[4]);
    int fd = open(path, O_RDONLY | O_DIRECT);
    if (fd < 0) { perror("open"); return 1; }
    std::vector<std::vector<double>> lat(threads);
    std::atomic<long long> done{0};
    auto t0 = std::chrono::steady_clock::now();
    std::vector<std::thread> ts;
    for (int t = 0; t < threads; ++t)
        ts.emplace_back([&, t] {
            void* buf = nullptr;
            posix_memalign(&buf, 4096, 4096);
            std::mt19937_64 rng(1234 + t);
            for (int i = 0; i < per; ++i) {
                const long long row = (long long) (rng() % 320001536ULL);
                const long long off = (data_start + row * 90) & ~4095LL;
                auto a = std::chrono::steady_clock::now();
                if (pread(fd, buf, 4096, off) != 4096) { perror("pread"); std::exit(1); }
                lat[t].push_back(std::chrono::duration<double, std::micro>(std::chrono::steady_clock::now() - a).count());
            }
            free(buf);
        });
    for (auto& x : ts) x.join();
    const double s = std::chrono::duration<double>(std::chrono::steady_clock::now() - t0).count();
    std::vector<double> all;
    for (auto& v : lat) all.insert(all.end(), v.begin(), v.end());
    std::sort(all.begin(), all.end());
    std::printf("threads=%d reads=%zu: %.0f IOPS, latency p50 %.0f us p90 %.0f us p99 %.0f us max %.0f us\n", threads,
                all.size(), all.size() / s, all[all.size() / 2], all[all.size() * 9 / 10], all[all.size() * 99 / 100], all.back());
    close(fd);
}
