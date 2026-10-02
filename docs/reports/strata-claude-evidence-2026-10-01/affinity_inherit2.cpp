// Variant: 2 pipeline stages x 4 stager threads + one launcher thread spinning on yield() (Stager::wait / issuer),
// created from an unpinned thread vs from a thread pinned to CPU 0 (SessionLoopScratch::init).
#include <pthread.h>
#include <sched.h>
#include <atomic>
#include <chrono>
#include <cstdio>
#include <cstring>
#include <thread>
#include <vector>
static void pin_to(int cpu) { cpu_set_t s; CPU_ZERO(&s); CPU_SET(cpu, &s); pthread_setaffinity_np(pthread_self(), sizeof s, &s); }
static double run(int threads, const unsigned char* src, size_t src_bytes) {
    const size_t blob = 2176000, jobs = (16ull << 30) / blob;
    std::vector<std::vector<unsigned char>> dst((size_t) threads, std::vector<unsigned char>(blob, 1));
    std::atomic<size_t> next{0};
    std::atomic<bool> stop{false};
    std::thread spinner([&] { while (!stop.load()) std::this_thread::yield(); });   // the launcher's wait loop
    auto t0 = std::chrono::steady_clock::now();
    std::vector<std::thread> ts;
    for (int t = 0; t < threads; ++t)
        ts.emplace_back([&, t] {
            for (;;) {
                const size_t j = next.fetch_add(1);
                if (j >= jobs) return;
                std::memcpy(dst[(size_t) t].data(), src + (j * 7919 % (src_bytes / blob)) * blob, blob);
            }
        });
    for (auto& t : ts) t.join();
    const double s = std::chrono::duration<double>(std::chrono::steady_clock::now() - t0).count();
    stop = true; spinner.join();
    return (double) jobs * blob / s / 1e9;
}
int main() {
    const size_t src_bytes = 2ull << 30;
    std::vector<unsigned char> src(src_bytes, 3);
    double f4 = run(4, src.data(), src_bytes), f8 = run(8, src.data(), src_bytes);
    pin_to(0);
    double p4 = run(4, src.data(), src_bytes), p8 = run(8, src.data(), src_bytes);
    std::printf("copy GB/s with a yield-spinning launcher: 1 stage(4 thr) free %.1f / after-pin %.1f; "
                "2 stages(8 thr) free %.1f / after-pin %.1f\n", f4, p4, f8, p8);
}
