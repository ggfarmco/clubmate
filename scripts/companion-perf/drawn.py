import re,sys
runs=[];cur=None
for line in open(sys.argv[1]):
    m=re.search(r'\[com\.clubmate\.companion\.perf:frame\] tick (\w+) (\d+) ms',line)
    if not m: continue
    t=int(m.group(2))
    if cur is None or t<cur[-1]: cur=[];runs.append(cur)
    cur.append(t)
lat=[]
for r in runs:
    prev=0;best=(0,r[0] if r else 0)
    for t in r:
        if t-prev>best[0]: best=(t-prev,t)
        prev=t
    lat.append(best[1])
lat.sort()
if not lat: sys.exit()
print(f'tap->drawn ms: n={len(lat)} median={lat[len(lat)//2]} p90={lat[int(len(lat)*0.9)]} max={lat[-1]}')
