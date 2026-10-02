// Synthetic reproduction of the Strata host-thread pin (src/core/session.cpp:538-541 pins the serve loop's thread to
// physical_cores(false)[0]) followed by worker creation after the pin (prefill Stager threads, prefill issuer,
// std::async PLE/next-run, adaptive tier thread). Linux: a thread inherits its creator's CPU affinity mask.
//
// Part 1: report the affinity of std::thread / std::async created before and after the pin.
// Part 2: 4 "stager" threads memcpy 2 MiB blobs from a 1 GiB source into pinned-like destination buffers,
//         created (a) from an unpinned thread, (b) from a thread pinned to one CPU. Report aggregate GB/s.
#include <pthread.h>
#include <sched.h>

#include <atomic>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <future>
#include <string>
#include <thread>
#include <vector>

static std::string mask_str() {
    cpu_set_t s;
    CPU_ZERO(&s);
    pthread_getaffinity_np(pthread_self(), sizeof s, &s);
    std::string out;
    int first = -1, prev = -2;
    auto flush = [&](int a, int b) {
        if (a < 0) return;
        if (!out.empty()) out += ",";
        out += a == b ? std::to_string(a) : std::to_string(a) + "-" + std::to_string(b);
    };
    for (int i = 0; i < CPU_SETSIZE; ++i)
        if (CPU_ISSET(i, &s)) {
            if (i != prev + 1) { flush(first, prev); first = i; }
            prev = i;
        }
    flush(first, prev);
    return out;
}

static void pin_to(int cpu) {
    cpu_set_t s;
    CPU_ZERO(&s);
    CPU_SET(cpu, &s);
    pthread_setaffinity_np(pthread_self(), sizeof s, &s);
}

static double copy_bench(int threads, size_t blob, size_t total_bytes, const unsigned char* src, size_t src_bytes) {
    std::vector<std::vector<unsigned char>> dst((size_t) threads, std::vector<unsigned char>(blob));
    for (auto& d : dst) std::memset(d.data(), 1, blob);
    std::atomic<size_t> next{0};
    const size_t jobs = total_bytes / blob;
    auto t0 = std::chrono::steady_clock::now();
    std::vector<std::thread> ts;
    for (int t = 0; t < threads; ++t)
        ts.emplace_back([&, t] {
            for (;;) {
                const size_t j = next.fetch_add(1);
                if (j >= jobs) return;
                const size_t off = (j * 7919 % (src_bytes / blob)) * blob;   // scattered experts
                std::memcpy(dst[(size_t) t].data(), src + off, blob);
            }
        });
    for (auto& t : ts) t.join();
    const double s = std::chrono::duration<double>(std::chrono::steady_clock::now() - t0).count();
    return (double) jobs * (double) blob / s / 1e9;
}

int main() {
    std::printf("main before pin: {%s}\n", mask_str().c_str());
    std::thread([] { std::printf("std::thread created before pin: {%s}\n", mask_str().c_str()); }).join();

    const size_t src_bytes = 1ull << 30, blob = 2176000;   // IQ3_XXS blob size from the pack's native_experts.txt
    std::vector<unsigned char> src(src_bytes);
    for (size_t i = 0; i < src_bytes; i += 4096) src[i] = (unsigned char) i;
    std::memset(src.data(), 3, src_bytes);

    const double free_bw = copy_bench(4, blob, 8ull << 30, src.data(), src_bytes);

    pin_to(0);   // what SessionLoopScratch::init does to the serve thread
    std::printf("main after pin: {%s}\n", mask_str().c_str());
    std::thread([] { std::printf("std::thread created after pin: {%s}\n", mask_str().c_str()); }).join();
    std::async(std::launch::async, [] { std::printf("std::async created after pin: {%s}\n", mask_str().c_str()); }).get();

    const double pinned_bw = copy_bench(4, blob, 8ull << 30, src.data(), src_bytes);
    std::printf("4 stager-like copy threads, 2.18 MB blobs: created unpinned %.1f GB/s; created after pin %.1f GB/s\n",
                free_bw, pinned_bw);
    return 0;
}
