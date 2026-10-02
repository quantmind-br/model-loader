// Small read-only-ish hardware probe (256 MiB per GPU): replicates strata's probe_pcie_h2d_gbps()
// (src/program/generate.cpp:914-941) per device, then measures pageable H2D (what --mmap-experts adapt/refill
// paths and the driver's staging use), concurrent H2D on both GPUs, and the peer-access capability.
#include <cuda_runtime.h>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <thread>
#include <vector>

#define CK(x) do { cudaError_t e = (x); if (e != cudaSuccess) { std::printf("%s: %s\n", #x, cudaGetErrorString(e)); std::exit(1); } } while (0)

constexpr size_t kBytes = 256ull << 20;

double strata_probe(int dev) {   // identical constants and sequence to probe_pcie_h2d_gbps()
    CK(cudaSetDevice(dev));
    void *h = nullptr, *d = nullptr;
    cudaEvent_t a, b;
    CK(cudaMallocHost(&h, kBytes));
    CK(cudaMalloc(&d, kBytes));
    CK(cudaEventCreate(&a));
    CK(cudaEventCreate(&b));
    std::memset(h, 0, kBytes);
    cudaMemcpyAsync(d, h, kBytes, cudaMemcpyHostToDevice);
    cudaEventRecord(a);
    for (int i = 0; i < 4; ++i) cudaMemcpyAsync(d, h, kBytes, cudaMemcpyHostToDevice);
    cudaEventRecord(b);
    cudaEventSynchronize(b);
    float ms = 0;
    cudaEventElapsedTime(&ms, a, b);
    cudaEventDestroy(a); cudaEventDestroy(b); cudaFree(d); cudaFreeHost(h);
    return 4.0 * kBytes / (ms * 1e-3) / 1e9;
}

double pageable_h2d(int dev, size_t chunk) {
    CK(cudaSetDevice(dev));
    std::vector<unsigned char> h(kBytes, 1);
    void* d = nullptr;
    CK(cudaMalloc(&d, kBytes));
    cudaMemcpy(d, h.data(), kBytes, cudaMemcpyHostToDevice);
    auto t0 = std::chrono::steady_clock::now();
    for (int i = 0; i < 4; ++i)
        for (size_t off = 0; off < kBytes; off += chunk) cudaMemcpy((char*) d + off, h.data() + off, chunk, cudaMemcpyHostToDevice);
    CK(cudaDeviceSynchronize());
    double s = std::chrono::duration<double>(std::chrono::steady_clock::now() - t0).count();
    cudaFree(d);
    return 4.0 * kBytes / s / 1e9;
}

int main() {
    int n = 0;
    CK(cudaGetDeviceCount(&n));
    for (int dev = 0; dev < n; ++dev) {
        cudaDeviceProp p{};
        CK(cudaGetDeviceProperties(&p, dev));
        int clk = 0; cudaDeviceGetAttribute(&clk, cudaDevAttrClockRate, dev);
        size_t fr = 0, tot = 0; cudaSetDevice(dev); cudaMemGetInfo(&fr, &tot);
        std::printf("CUDA%d %s sm_%d%d SMs=%d clock(attr)=%.3f GHz pci=%02x:%02x free=%.2f GiB of %.2f\n", dev, p.name,
                    p.major, p.minor, p.multiProcessorCount, clk / 1e6, p.pciBusID, p.pciDeviceID, fr / 1073741824.0,
                    tot / 1073741824.0);
    }
    for (int dev = 0; dev < n; ++dev) {
        const double bw = strata_probe(dev);
        const double frac = bw >= 20.0 ? 0.55 : bw < 4.0 ? 0.0 : std::min(0.55, std::max(0.05, 0.55 * (bw / 26.0)));
        std::printf("CUDA%d strata-style pinned H2D probe: %.2f GB/s -> pcie_frac %.3f (engine formula)\n", dev, bw, frac);
        std::printf("CUDA%d pageable H2D, 2.18 MB copies: %.2f GB/s\n", dev, pageable_h2d(dev, 2176000));
    }
    if (n >= 2) {
        double r[2];
        std::thread t0([&] { r[0] = strata_probe(0); });
        std::thread t1([&] { r[1] = strata_probe(1); });
        t0.join(); t1.join();
        std::printf("concurrent pinned H2D: CUDA0 %.2f GB/s + CUDA1 %.2f GB/s = %.2f GB/s\n", r[0], r[1], r[0] + r[1]);
        int a = 0, b = 0;
        cudaDeviceCanAccessPeer(&a, 0, 1);
        cudaDeviceCanAccessPeer(&b, 1, 0);
        std::printf("cudaDeviceCanAccessPeer 0->1 %d, 1->0 %d\n", a, b);
    }
    return 0;
}
