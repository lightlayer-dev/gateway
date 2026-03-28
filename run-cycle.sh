#!/bin/bash
# Gateway build cycle runner (Go)
# Usage: ./run-cycle.sh <cycle-number>

CYCLE=$1
REPO_DIR="/root/.openclaw/workspace/gateway"
PROMPT_FILE="/tmp/gateway-go-cycle-${CYCLE}.md"

if [ ! -f "$PROMPT_FILE" ]; then
  echo "No prompt file for cycle $CYCLE"
  exit 1
fi

cd "$REPO_DIR"

# Pull latest main
git checkout main 2>/dev/null
git pull origin main 2>/dev/null

# Read the prompt
PROMPT=$(cat "$PROMPT_FILE")

# Add notification
PROMPT="${PROMPT}

When completely finished, run this command to notify me:
openclaw system event --text \"Gateway Cycle ${CYCLE} done\" --mode now"

# Run Claude Code
claude --allowedTools "Bash(*),Read,Write,Edit,Glob,Grep" --print "$PROMPT"

# After Claude Code finishes, try to merge the PR
echo "Cycle $CYCLE Claude Code finished. Checking for open PRs..."
sleep 10

PR_NUM=$(gh pr list --repo LightLayer-dev/gateway --state open --limit 1 --json number --jq '.[0].number' 2>/dev/null)

if [ -n "$PR_NUM" ] && [ "$PR_NUM" != "null" ]; then
  echo "Found PR #$PR_NUM, waiting for CI..."
  for i in $(seq 1 30); do
    STATUS=$(gh pr checks "$PR_NUM" --repo LightLayer-dev/gateway 2>/dev/null | grep -c "pass" || true)
    FAIL=$(gh pr checks "$PR_NUM" --repo LightLayer-dev/gateway 2>/dev/null | grep -c "fail" || true)
    PENDING=$(gh pr checks "$PR_NUM" --repo LightLayer-dev/gateway 2>/dev/null | grep -c "pending" || true)
    
    if [ "$FAIL" -gt 0 ]; then
      echo "CI failed on PR #$PR_NUM — not merging"
      break
    fi
    
    if [ "$PENDING" -eq 0 ] && [ "$STATUS" -gt 0 ]; then
      echo "CI passed! Merging PR #$PR_NUM"
      gh pr merge "$PR_NUM" --repo LightLayer-dev/gateway --merge --delete-branch 2>/dev/null
      break
    fi
    
    echo "Waiting for CI... (attempt $i/30)"
    sleep 10
  done
fi

echo "Cycle $CYCLE complete."
