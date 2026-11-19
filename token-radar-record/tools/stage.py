# Stage a copy of the Claude config directory for the transcript bundle: the session log as it is, and each
# instruction file (stock skill) with its front matter kept and its body replaced by a redaction note.
import os, shutil, sys
home, stage = '/root/.claude', sys.argv[1]
shutil.rmtree(stage, ignore_errors=True)
proj = 'projects/-home-user-make-fun'
os.makedirs(os.path.join(stage, proj))
for f in os.listdir(os.path.join(home, proj)):
    p = os.path.join(home, proj, f)
    if os.path.isfile(p): shutil.copy2(p, os.path.join(stage, proj, f))
    else: shutil.copytree(p, os.path.join(stage, proj, f))
n = 0
for root, _, files in os.walk(os.path.join(home, 'skills')):
    for f in files:
        if f != 'SKILL.md': continue
        text = open(os.path.join(root, f), encoding='utf-8').read()
        head = ''
        if text.startswith('---'):
            end = text.find('\n---', 3)
            if end > 0: head = text[:end + 4] + '\n\n'
        out = os.path.join(stage, os.path.relpath(root, home))
        os.makedirs(out, exist_ok=True)
        open(os.path.join(out, f), 'w', encoding='utf-8').write(head + '[REDACTED]\n\nThe body of this skill file is redacted. It is a stock skill the harness syncs to the account, not written for this build, and the build did not invoke it; its text is its publisher\'s, not the entrant\'s to pass on.\n')
        n += 1
print('staged', n, 'skill files and', len(os.listdir(os.path.join(stage, proj))), 'project entries')
