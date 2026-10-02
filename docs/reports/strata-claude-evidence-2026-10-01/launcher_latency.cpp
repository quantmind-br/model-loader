// Launcher-latency under co-scheduling: one "launcher" performs 2000 short steps (~20 us of work each, like a burst
// of kernel launches) while N memcpy threads run (a) on other CPUs, (b) on the launcher's CPU (inherited pin).
#include <pthread.h>
#include <atomic>
#include <chrono>
#include <cstdio>
#include <cstring>
#include <thread>
#include <vector>
#include <algorithm>
static void pin_to(int cpu) { cpu_set_t s; CPU_ZERO(&s); CPU_SET(cpu, &s); pthread_setaffinity_np(pthread_self(), sizeof s, &s); }
static void pin_set(int a, int b) { cpu_set_t s; CPU_ZERO(&s); for (int i = a; i <= b; ++i) CPU_SET(i, &s); pthread_setaffinity_np(pthread_self(), sizeof s, &s); }
int main() {
    const size_t src_bytes = 1ull << 30, blob = 2176000;
    std::vector<unsigned char> src(src_bytes, 3);
    auto scenario = [&](const char* name, int copiers, bool same_cpu) {
        std::atomic<bool> stop{false};
        std::vector<std::thread> cs;
        for (int t = 0; t < copiers; ++t)
            cs.emplace_back([&, t] {
                if (same_cpu) pin_to(0); else pin_set(16, 31);
                std::vector<unsigned char> d(blob, 1);
                size_t j = t;
                while (!stop.load(std::memory_order_relaxed)) { std::memcpy(d.data(), src.data() + (j * 7919 % (src_bytes / blob)) * blob, blob); ++j; }
            });
        std::vector<double> lat;
        std::thread l([&] {
            pin_to(0);
            std::this_thread::sleep_for(std::chrono::milliseconds(50));
            for (int i = 0; i < 2000; ++i) {
                auto a = std::chrono::steady_clock::now();
                volatile double x = 1; while (std::chrono::steady_clock::now() - a < std::chrono::microseconds(20)) x = x * 1.0000001;
                lat.push_back(std::chrono::duration<double, std::micro>(std::chrono::steady_clock::now() - a).count());
                std::this_thread::yield();
            }
        });
        auto t0 = std::chrono::steady_clock::now();
        l.join();
        double wall = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - t0).count();
        stop = true;
        for (auto& c : cs) c.join();
        std::sort(lat.begin(), lat.end());
        std::printf("%-34s 2000 x 20us steps: wall %7.1f ms (ideal ~40), step p50 %6.1f us, p99 %7.1f us, max %8.1f us\n",
                    name, wall, lat[1000], lat[1980], lat.back());
    };
    scenario("launcher alone", 0, false);
    scenario("4 copiers on other CPUs", 4, false);
    scenario("4 copiers on launcher CPU", 4, true);
    scenario("8 copiers on launcher CPU", 8, true);
}
