#!/bin/sh
# Wait until the repository's GitHub About carries the make.fun website and the makefun topic, then
# rebuild the transcript bundle and submit the entry. Polls the public repository page, no credentials.
S=${S:?set S to the scratch directory that holds fetched/ and stage/}
i=0
while [ $i -lt 80 ]; do
  page=$(curl -s "https://github.com/karthicksiv/make-fun?t=$(date +%s)")
  if echo "$page" | grep -q 'href="https://make.fun' && echo "$page" | grep -q '/topics/makefun"'; then
    echo "About is set (after $((i * 20)) s): submitting"
    "$S/tools/bundle.sh" | tail -1
    cd "$S/fetched/kit/token-radar-01-kit" && node cli/makefun.mjs submit --competition token-radar-01 \
      --repo-url https://github.com/karthicksiv/make-fun --commit faf0349d5ded2294165a7bd06251ab814f81699e \
      --title "Launch Radar Terminal" --bundle /home/user/make-fun/token-radar-01/transcripts.tar.gz \
      --keypair /root/.makefun/keypairs/https_make.fun.json 2>&1 | sed -E 's/af_[A-Za-z0-9_-]+/af_<hidden>/g' | cut -c1-900 | tail -40
    exit 0
  fi
  i=$((i + 1)); sleep 20
done
echo "TIMEOUT: the About still has no make.fun website and makefun topic after 27 minutes"; exit 3
