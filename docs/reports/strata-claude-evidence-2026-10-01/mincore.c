#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>
int main(int argc, char** argv) {
    long page = sysconf(_SC_PAGESIZE);
    for (int i = 1; i < argc; ++i) {
        int fd = open(argv[i], O_RDONLY);
        struct stat st; fstat(fd, &st);
        void* p = mmap(NULL, st.st_size, PROT_READ, MAP_SHARED, fd, 0);   /* no page is touched */
        size_t pages = (st.st_size + page - 1) / page;
        unsigned char* v = malloc(pages);
        if (mincore(p, st.st_size, v) != 0) { perror("mincore"); return 1; }
        size_t r = 0; for (size_t k = 0; k < pages; ++k) r += v[k] & 1;
        printf("%s: %.2f of %.2f GiB resident in page cache (%.0f%%)\n", argv[i], r * (double) page / (1 << 30),
               st.st_size / (double) (1 << 30), 100.0 * r / pages);
        munmap(p, st.st_size); close(fd); free(v);
    }
}
