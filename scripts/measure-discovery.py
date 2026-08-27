import os,json,glob,re,collections,subprocess
root=os.path.expanduser('~/.claude/projects')
def slug(p): return re.sub(r'[^A-Za-z0-9-]','-',p)
uuidre=re.compile(r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$')
dirs=[d for d in sorted(os.listdir(root)) if os.path.isdir(os.path.join(root,d))]
total=0;nonuuid=0;empty=0;nocwd=0;chars=collections.Counter();dirchars=collections.Counter()
firstok=0;firstbad=0;verhist=collections.Counter();gone=0;badjson=0
for d in dirs:
    for ch in d:
        if not (ch.isalnum() or ch=='-'): dirchars[ch]+=1
    for f in sorted(glob.glob(os.path.join(root,d,'*.jsonl'))):
        total+=1
        if not uuidre.match(os.path.basename(f)): nonuuid+=1
        if os.path.getsize(f)==0: empty+=1; continue
        first=None; cands=set(); seen=False
        for line in open(f,errors='replace'):
            line=line.strip()
            if not line: continue
            try: o=json.loads(line)
            except Exception: badjson+=1; continue
            if not isinstance(o,dict): continue
            c=o.get('cwd')
            if not isinstance(c,str): continue
            seen=True
            for ch in c:
                if not (ch.isalnum() or ch=='-'): chars[ch]+=1
            if first is None: first=c
            if slug(c)==d: cands.add(c)
        if not seen: nocwd+=1; continue
        if slug(first)==d: firstok+=1
        else: firstbad+=1
        live={c for c in cands if os.path.isdir(c)}
        verhist[len(live)]+=1
        if cands and not live: gone+=1
print(f"project dirs                          {len(dirs)}")
print(f"transcripts                           {total}")
print(f"  filename not <uuid>.jsonl           {nonuuid}")
print(f"  zero bytes                          {empty}")
print(f"  no cwd on any line                  {nocwd}")
print(f"  lines that are not JSON             {badjson}")
print(f"first cwd slugs to its own dir        ok {firstok}  MISMATCH {firstbad}")
print(f"verified dirs per transcript          {dict(sorted(verhist.items()))}")
print(f"  matched a cwd, dir now deleted      {gone}")
print(f"non-[A-Za-z0-9-] chars in cwd values  {dict(chars)}")
print(f"non-[A-Za-z0-9-] chars in slug dirs   {dict(dirchars)}")
out=subprocess.run(['ps','-Aww','-o','command='],capture_output=True,text=True).stdout
bare=sid=res=0
for l in out.splitlines():
    a=l.split()
    if not a: continue
    if os.path.basename(a[0])!='claude': continue
    if len(a)>1 and a[1] in ('bg-pty-host','bg-spare','daemon'): continue
    if '--session-id' in a: sid+=1
    elif '--resume' in a: res+=1
    else: bare+=1
print(f"live interactive claude processes      bare {bare}  --session-id {sid}  --resume {res}")
