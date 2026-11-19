# Stage the instruction files for the transcript bundle. A stock skill under the Apache License 2.0 goes up
# whole, with its license beside it. Any other stock skill goes up with its name and description and a note
# in place of its body, since the entrant holds no license to pass its text on. The real files never change.
import os, shutil, sys
home, stage = '/root/.claude', sys.argv[1]
shutil.rmtree(stage, ignore_errors=True)
whole, noted = [], []
for root, _, files in os.walk(os.path.join(home, 'skills')):
    if 'SKILL.md' not in files: continue
    name = os.path.basename(root)
    rel = os.path.relpath(root, home).split(os.sep)
    # the synced folder's own name is an account identifier: stage it under a neutral one
    if len(rel) > 2 and rel[1] == 'synced': rel[2] = 'account'
    out = os.path.join(stage, *rel); os.makedirs(out, exist_ok=True)
    text = open(os.path.join(root, 'SKILL.md'), encoding='utf-8').read()
    lic = os.path.join(root, 'LICENSE.txt')
    apache = os.path.exists(lic) and 'Apache License' in open(lic, encoding='utf-8').read(400)
    if apache:
        open(os.path.join(out, 'SKILL.md'), 'w', encoding='utf-8').write(text)
        shutil.copy2(lic, os.path.join(out, 'LICENSE.txt'))
        whole.append(name)
    else:
        head = ''
        if text.startswith('---'):
            end = text.find('\n---', 3)
            if end > 0: head = text[:end + 4] + '\n\n'
        open(os.path.join(out, 'SKILL.md'), 'w', encoding='utf-8').write(head + 'The body of this skill file is not uploaded. It is a stock skill the harness syncs to the account: it was not written for this build, and the build did not invoke it. The entrant holds no license to pass its text on to a third party' + (' (its license file forbids that in terms)' if os.path.exists(lic) else '') + ', so only its name and description are given above.\n')
        noted.append(name)
print('whole:', sorted(whole)); print('name, description and note:', sorted(noted))
