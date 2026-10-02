import re, pathlib
root = pathlib.Path('/home/diogo/dev/model-loader')
text = (root/'IDEATION_PERFORMANCE.md').read_text()
cites = re.findall(r'`((?:backends|internal)/[^`:]+):(\d+)`', text)
seen=set()
for path, line in cites:
    if (path,line) in seen: continue
    seen.add((path,line))
    p = root/path
    if not p.exists():
        print('MISSING', path); continue
    lines = p.read_text(errors='replace').splitlines()
    n=int(line)
    s = lines[n-1].strip() if n<=len(lines) else '<<OUT OF RANGE>>'
    print(f'{path}:{line}: {s[:150]}')
print(len(seen),'citações únicas')
