#!/bin/bash
CYCLE=$1
REPO_DIR="/root/.openclaw/workspace/gateway"
PROMPT_FILE="/tmp/gateway-go-cycle-${CYCLE}.md"
if [ ! -f "$PROMPT_FILE" ]; then echo "No prompt file for cycle $CYCLE"; exit 1; fi
cd "$REPO_DIR"
git checkout main 2>/dev/null; git pull origin main 2>/dev/null
PROMPT=$(cat "$PROMPT_FILE")
PROMPT="${PROMPT}

When completely finished, run this command to notify me:
openclaw system event --text \"Gateway Cycle ${CYCLE} done\" --mode now"
claude --allowedTools "Bash(*),Read,Write,Edit,Glob,Grep" --print "$PROMPT"
echo "Cycle $CYCLE Claude Code finished. Checking for open PRs..."
sleep 10
PR_NUM=$(gh pr list --repo LightLayer-dev/gateway --state open --limit 1 --json number --jq '.[0].number' 2>/dev/null)
if [ -n "$PR_NUM" ] && [ "$PR_NUM" != "null" ]; then
  echo "Found PR #$PR_NUM, waiting for CI..."
  for i in $(seq 1 30); do
    FAIL=$(gh pr checks "$PR_NUM" --repo LightLayer-dev/gateway 2>/dev/null | grep -c "fail" || true)
    PENDING=$(gh pr checks "$PR_NUM" --repo LightLayer-dev/gateway 2>/dev/null | grep -c "pending" || true)
    if [ "$FAIL" -gt 0 ]; then echo "CI failed"; break; fi
    if [ "$PENDING" -eq 0 ]; then
      echo "CI passed! Merging PR #$PR_NUM"
      gh pr merge "$PR_NUM" --repo LightLayer-dev/gateway --merge --delete-branch 2>/dev/null
      break
    fi
    sleep 10
  done
fi
