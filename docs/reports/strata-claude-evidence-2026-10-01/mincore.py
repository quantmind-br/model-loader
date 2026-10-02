import ctypes, mmap, os, sys
libc = ctypes.CDLL("libc.so.6", use_errno=True)
libc.mincore.argtypes = [ctypes.c_void_p, ctypes.c_size_t, ctypes.c_void_p]
PAGE = os.sysconf("SC_PAGE_SIZE")
for path in sys.argv[1:]:
    size = os.path.getsize(path)
    fd = os.open(path, os.O_RDONLY)
    resident = 0
    chunk = 1 << 30
    for off in range(0, size, chunk):
        n = min(chunk, size - off)
        m = mmap.mmap(fd, n, mmap.MAP_SHARED, mmap.PROT_READ, offset=off)
        addr = ctypes.c_void_p.from_buffer(m) if False else None
        buf = (ctypes.c_char * n).from_buffer_copy(b"") if False else None
        p = ctypes.c_void_p(ctypes.addressof(ctypes.c_char.from_buffer(m)))
        vec = (ctypes.c_ubyte * ((n + PAGE - 1) // PAGE))()
        if libc.mincore(p, n, vec) != 0:
            raise OSError(ctypes.get_errno(), "mincore")
        resident += sum(v & 1 for v in vec) * PAGE
        del p
        m.close()
    os.close(fd)
    print(f"{path}: {resident/2**30:.2f} of {size/2**30:.2f} GiB in page cache ({100*resident/size:.0f}%)")
