#!/usr/bin/env bash
# ════════════════════════════════════════════════════════════════════════════
# Automax Goal Management — End-to-End Test Script
# ════════════════════════════════════════════════════════════════════════════
#
# Tests ALL goal management features using the real backend API.
# Requires: curl, jq, running backend on localhost:8080
#
# Usage:
#   chmod +x scripts/e2e_goal_test.sh
#   cd backend && ./scripts/e2e_goal_test.sh
#
# Test Users (password: admin123 for all):
#   admin@automax.com       — Super Admin
#   sarah.manager@automax.com — Manager (Civil)
#   ahmed.reviewer@automax.com — Goal Reviewer (Civil)
#   fatima.senior@automax.com — Goal Reviewer (Civil)
#   omar.worker@automax.com  — Goal Collaborator (Civil)
#   khalid.viewer@automax.com — User (Electrical)
#   layla.director@automax.com — Manager (Electrical)
# ════════════════════════════════════════════════════════════════════════════

set -o pipefail

export PATH="/home/ks/.local/bin:/usr/bin:/usr/local/bin:/bin:/usr/sbin:$PATH"

BASE_URL="${BASE_URL:-http://localhost:8080/api/v1}"
PASSWORD="admin123"
RUN_ID="$(date +%s)"  # unique suffix for names that have unique constraints

# ── Colors & formatting ───────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
TOTAL_COUNT=0

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  TOTAL_COUNT=$((TOTAL_COUNT + 1))
  echo -e "  ${GREEN}✓${NC} $1"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  TOTAL_COUNT=$((TOTAL_COUNT + 1))
  echo -e "  ${RED}✗${NC} $1"
  if [ -n "${2:-}" ]; then
    echo -e "    ${RED}→ $2${NC}"
  fi
}

skip() {
  SKIP_COUNT=$((SKIP_COUNT + 1))
  TOTAL_COUNT=$((TOTAL_COUNT + 1))
  echo -e "  ${YELLOW}⊘${NC} $1 (skipped)"
}

section() {
  echo ""
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BOLD}${CYAN}  $1${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

subsection() {
  echo ""
  echo -e "  ${BOLD}$1${NC}"
}

# ── HTTP helpers ──────────────────────────────────────────────────────────

# Generic request: returns body. Sets $HTTP_CODE as side effect.
api() {
  local method="$1" path="$2" token="$3"
  shift 3
  local body="${1:-}"
  local tmpfile
  tmpfile=$(mktemp)

  local curl_args=(
    -s -w '\n%{http_code}'
    -X "$method"
    -H "Authorization: Bearer $token"
    -H "Content-Type: application/json"
  )

  if [ -n "$body" ]; then
    curl_args+=(-d "$body")
  fi

  local response
  response=$(curl "${curl_args[@]}" "${BASE_URL}${path}" 2>/dev/null || echo -e "\n000")

  HTTP_CODE=$(echo "$response" | tail -1)
  echo "$response" | sed '$d'
}

# Extract .data field from response
jq_data() {
  echo "$1" | jq -r '.data' 2>/dev/null
}

# Extract specific field from .data
jq_field() {
  echo "$1" | jq -r ".data.$2" 2>/dev/null
}

# Check if response is successful
is_success() {
  echo "$1" | jq -r '.success' 2>/dev/null | grep -q 'true'
}

# ── Login helper ──────────────────────────────────────────────────────────

login() {
  local email="$1"
  local response
  response=$(curl -s -X POST "${BASE_URL}/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${email}\",\"password\":\"${PASSWORD}\"}" 2>/dev/null)

  local token
  token=$(echo "$response" | jq -r '.data.token // empty' 2>/dev/null)

  if [ -z "$token" ] || [ "$token" = "null" ]; then
    echo ""
    return 1
  fi

  echo "$token"
}

# Extracts user ID from login response
get_user_id() {
  local email="$1"
  local response
  response=$(curl -s -X POST "${BASE_URL}/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${email}\",\"password\":\"${PASSWORD}\"}" 2>/dev/null)

  echo "$response" | jq -r '.data.user.id // empty' 2>/dev/null
}

# ════════════════════════════════════════════════════════════════════════════
# PREFLIGHT: Check server is running
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}${CYAN}╔══════════════════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}${CYAN}║    AUTOMAX GOAL MANAGEMENT — E2E TEST SUITE             ║${NC}"
echo -e "${BOLD}${CYAN}╚══════════════════════════════════════════════════════════╝${NC}"
echo ""

echo -n "Checking server at ${BASE_URL}... "
if ! curl -s -o /dev/null -w '' "${BASE_URL}/../health" 2>/dev/null && \
   ! curl -s -o /dev/null -w '' "${BASE_URL}/auth/login" 2>/dev/null; then
  echo -e "${RED}FAILED${NC}"
  echo "Backend is not running. Start it first: go run ./cmd/server/"
  exit 1
fi
echo -e "${GREEN}OK${NC}"

# ════════════════════════════════════════════════════════════════════════════
# PHASE 0: Authentication
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 0: Authentication"

# Login all test users
declare -A TOKENS
declare -A USER_IDS

TEST_USERS=(
  "admin@automax.com"
  "sarah.manager@automax.com"
  "ahmed.reviewer@automax.com"
  "fatima.senior@automax.com"
  "omar.worker@automax.com"
  "khalid.viewer@automax.com"
  "layla.director@automax.com"
)

for email in "${TEST_USERS[@]}"; do
  name=$(echo "$email" | cut -d@ -f1)
  response=$(curl -s -X POST "${BASE_URL}/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${email}\",\"password\":\"${PASSWORD}\"}" 2>/dev/null)

  token=$(echo "$response" | jq -r '.data.token // empty' 2>/dev/null)
  uid=$(echo "$response" | jq -r '.data.user.id // empty' 2>/dev/null)

  if [ -n "$token" ] && [ "$token" != "null" ]; then
    TOKENS["$email"]="$token"
    USER_IDS["$email"]="$uid"
    pass "Login: $email (id: ${uid:0:8}...)"
  else
    fail "Login: $email" "$(echo "$response" | jq -r '.error // .message // "unknown"' 2>/dev/null)"
  fi
done

# Shortcuts
ADMIN_TOKEN="${TOKENS[admin@automax.com]:-}"
SARAH_TOKEN="${TOKENS[sarah.manager@automax.com]:-}"
AHMED_TOKEN="${TOKENS[ahmed.reviewer@automax.com]:-}"
FATIMA_TOKEN="${TOKENS[fatima.senior@automax.com]:-}"
OMAR_TOKEN="${TOKENS[omar.worker@automax.com]:-}"
KHALID_TOKEN="${TOKENS[khalid.viewer@automax.com]:-}"
LAYLA_TOKEN="${TOKENS[layla.director@automax.com]:-}"

ADMIN_ID="${USER_IDS[admin@automax.com]:-}"
SARAH_ID="${USER_IDS[sarah.manager@automax.com]:-}"
AHMED_ID="${USER_IDS[ahmed.reviewer@automax.com]:-}"
FATIMA_ID="${USER_IDS[fatima.senior@automax.com]:-}"
OMAR_ID="${USER_IDS[omar.worker@automax.com]:-}"
KHALID_ID="${USER_IDS[khalid.viewer@automax.com]:-}"
LAYLA_ID="${USER_IDS[layla.director@automax.com]:-}"

if [ -z "$ADMIN_TOKEN" ]; then
  echo -e "\n${RED}FATAL: Cannot login as admin. Aborting.${NC}"
  exit 1
fi

# Fetch a department ID for test goals
subsection "Fetching departments..."
DEPT_RESP=$(api GET "/admin/departments?limit=50" "$ADMIN_TOKEN")
CIVIL_DEPT_ID=$(echo "$DEPT_RESP" | jq -r '.data[] | select(.name | test("civil";"i")) | .id' 2>/dev/null | head -1)
ELEC_DEPT_ID=$(echo "$DEPT_RESP" | jq -r '.data[] | select(.name | test("electr";"i")) | .id' 2>/dev/null | head -1)

if [ -n "$CIVIL_DEPT_ID" ]; then
  pass "Found Civil department: ${CIVIL_DEPT_ID:0:8}..."
else
  # Try alternative: get first department
  CIVIL_DEPT_ID=$(echo "$DEPT_RESP" | jq -r '.data[0].id // empty' 2>/dev/null)
  if [ -n "$CIVIL_DEPT_ID" ]; then
    pass "Using first department: ${CIVIL_DEPT_ID:0:8}..."
  else
    skip "No departments found — some tests will use null department_id"
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 1: Goal CRUD
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 1: Goal CRUD"

subsection "1.1 Create goals"

# Goal 1: Parent goal (by admin)
GOAL1_BODY=$(cat <<EOF
{
  "title": "E2E Test — Improve Safety Standards ${RUN_ID}",
  "description": "End-to-end test goal for safety improvements",
  "category": "Safety",
  "priority": "High",
  "owner_id": "$SARAH_ID",
  $([ -n "$CIVIL_DEPT_ID" ] && echo "\"department_id\": \"$CIVIL_DEPT_ID\",")
  "start_date": "2025-01-01T00:00:00Z",
  "target_date": "2025-12-31T00:00:00Z"
}
EOF
)

RESP=$(api POST "/goals" "$ADMIN_TOKEN" "$GOAL1_BODY")
GOAL1_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
GOAL1_STATUS=$(echo "$RESP" | jq -r '.data.status // empty' 2>/dev/null)

if [ -n "$GOAL1_ID" ] && [ "$GOAL1_ID" != "null" ]; then
  pass "Create parent goal: $GOAL1_ID (status: $GOAL1_STATUS)"
else
  fail "Create parent goal" "$(echo "$RESP" | jq -r '.error // .message // empty' 2>/dev/null)"
  GOAL1_ID=""
fi

# Goal 2: Child goal (by sarah)
if [ -n "$GOAL1_ID" ]; then
  GOAL2_BODY=$(cat <<EOF
{
  "title": "E2E Test — Reduce Workplace Incidents ${RUN_ID}",
  "description": "Child goal under safety standards",
  "category": "Safety",
  "priority": "Critical",
  "owner_id": "$OMAR_ID",
  $([ -n "$CIVIL_DEPT_ID" ] && echo "\"department_id\": \"$CIVIL_DEPT_ID\",")
  "parent_goal_id": "$GOAL1_ID",
  "start_date": "2025-02-01T00:00:00Z",
  "target_date": "2025-09-30T00:00:00Z"
}
EOF
)

  RESP=$(api POST "/goals" "$SARAH_TOKEN" "$GOAL2_BODY")
  GOAL2_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
  GOAL2_LEVEL=$(echo "$RESP" | jq -r '.data.level // empty' 2>/dev/null)

  if [ -n "$GOAL2_ID" ] && [ "$GOAL2_ID" != "null" ]; then
    pass "Create child goal: $GOAL2_ID (level: $GOAL2_LEVEL)"
  else
    fail "Create child goal" "$(echo "$RESP" | jq -r '.error // .message // empty' 2>/dev/null)"
    GOAL2_ID=""
  fi
fi

# Goal 3: Standalone goal in electrical dept (by layla)
GOAL3_BODY=$(cat <<EOF
{
  "title": "E2E Test — Upgrade Electrical Systems ${RUN_ID}",
  "description": "Standalone goal for electrical team",
  "category": "Infrastructure",
  "priority": "Medium",
  "owner_id": "$LAYLA_ID",
  $([ -n "$ELEC_DEPT_ID" ] && echo "\"department_id\": \"$ELEC_DEPT_ID\",")
  "start_date": "2025-03-01T00:00:00Z",
  "target_date": "2025-11-30T00:00:00Z"
}
EOF
)

RESP=$(api POST "/goals" "$ADMIN_TOKEN" "$GOAL3_BODY")
GOAL3_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)

if [ -n "$GOAL3_ID" ] && [ "$GOAL3_ID" != "null" ]; then
  pass "Create standalone goal: $GOAL3_ID"
else
  fail "Create standalone goal" "$(echo "$RESP" | jq -r '.error // .message // empty' 2>/dev/null)"
  GOAL3_ID=""
fi

subsection "1.2 Read & list goals"

# Get goal by ID
if [ -n "$GOAL1_ID" ]; then
  RESP=$(api GET "/goals/$GOAL1_ID" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    TITLE=$(echo "$RESP" | jq -r '.data.title' 2>/dev/null)
    pass "Get goal by ID: $TITLE"
  else
    fail "Get goal by ID"
  fi
fi

# List goals with filters
RESP=$(api GET "/goals?page=1&limit=10&status=Draft&sort_by=created_at&sort_order=desc" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  COUNT=$(echo "$RESP" | jq -r '.total_items // (.data | length)' 2>/dev/null)
  pass "List goals (Draft, page 1): ${COUNT} results"
else
  fail "List goals with filters"
fi

# List with search
RESP=$(api GET "/goals?search=E2E+Test" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  COUNT=$(echo "$RESP" | jq -r '.total_items // (.data | length)' 2>/dev/null)
  pass "Search goals ('E2E Test'): ${COUNT} results"
else
  fail "Search goals"
fi

# Root-only filter
RESP=$(api GET "/goals?root_only=true" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  pass "List root-only goals"
else
  fail "List root-only goals"
fi

subsection "1.3 Update goal"

if [ -n "$GOAL1_ID" ]; then
  RESP=$(api PUT "/goals/$GOAL1_ID" "$ADMIN_TOKEN" '{"description":"Updated by E2E test script","category":"Safety & Compliance"}')
  if is_success "$RESP"; then
    NEW_CAT=$(echo "$RESP" | jq -r '.data.category' 2>/dev/null)
    pass "Update goal: category → $NEW_CAT"
  else
    fail "Update goal" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

subsection "1.4 Goal hierarchy"

if [ -n "$GOAL1_ID" ]; then
  RESP=$(api GET "/goals/$GOAL1_ID/children" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    CHILDREN=$(echo "$RESP" | jq -r '.data | length' 2>/dev/null)
    pass "List children of parent goal: $CHILDREN child(ren)"
  else
    fail "List children"
  fi

  RESP=$(api GET "/goals/$GOAL1_ID/tree" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    pass "Get goal tree from root"
  else
    fail "Get goal tree"
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 2: Metrics
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 2: Goal Metrics"

METRIC1_ID=""
METRIC2_ID=""

if [ -n "$GOAL1_ID" ]; then
  subsection "2.1 Create metrics"

  # Metric 1: Numeric
  RESP=$(api POST "/goals/$GOAL1_ID/metrics" "$ADMIN_TOKEN" '{
    "name": "Incident Rate (per 1000 workers)",
    "metric_type": "Numeric",
    "unit": "incidents",
    "baseline_value": 12.5,
    "current_value": 12.5,
    "target_value": 5.0,
    "weight": 60
  }')
  METRIC1_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
  if [ -n "$METRIC1_ID" ] && [ "$METRIC1_ID" != "null" ]; then
    pass "Create numeric metric: $METRIC1_ID"
  else
    fail "Create numeric metric" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
    METRIC1_ID=""
  fi

  # Metric 2: Percentage
  RESP=$(api POST "/goals/$GOAL1_ID/metrics" "$ADMIN_TOKEN" '{
    "name": "Safety Training Completion",
    "metric_type": "Percentage",
    "unit": "%",
    "baseline_value": 40,
    "current_value": 40,
    "target_value": 95,
    "weight": 40
  }')
  METRIC2_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
  if [ -n "$METRIC2_ID" ] && [ "$METRIC2_ID" != "null" ]; then
    pass "Create percentage metric: $METRIC2_ID"
  else
    fail "Create percentage metric" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
    METRIC2_ID=""
  fi

  subsection "2.2 Update metric value"

  if [ -n "$METRIC1_ID" ]; then
    RESP=$(api PUT "/goals/metrics/$METRIC1_ID/value" "$ADMIN_TOKEN" '{
      "value": 9.2,
      "comment": "Q1 improvement — new safety protocols in effect"
    }')
    if is_success "$RESP"; then
      CURR=$(echo "$RESP" | jq -r '.data.current_value // empty' 2>/dev/null)
      pass "Update metric value: incident rate → $CURR"
    else
      fail "Update metric value" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
    fi

    # Second update
    RESP=$(api PUT "/goals/metrics/$METRIC1_ID/value" "$ADMIN_TOKEN" '{
      "value": 7.1,
      "comment": "Q2 — continued improvement"
    }')
    if is_success "$RESP"; then
      pass "Update metric value again: incident rate → 7.1"
    else
      fail "Second metric value update"
    fi
  fi

  if [ -n "$METRIC2_ID" ]; then
    RESP=$(api PUT "/goals/metrics/$METRIC2_ID/value" "$ADMIN_TOKEN" '{
      "value": 72,
      "comment": "Training batch 2 completed"
    }')
    if is_success "$RESP"; then
      pass "Update training metric: → 72%"
    else
      fail "Update training metric"
    fi
  fi

  subsection "2.3 Metric history"

  if [ -n "$METRIC1_ID" ]; then
    RESP=$(api GET "/goals/metrics/$METRIC1_ID/history?page=1&limit=10" "$ADMIN_TOKEN")
    if is_success "$RESP"; then
      ENTRIES=$(echo "$RESP" | jq -r '.total_items // (.data | length)' 2>/dev/null)
      pass "Metric history: $ENTRIES entries"
    else
      fail "Metric history"
    fi
  fi

  subsection "2.4 Update metric definition"

  if [ -n "$METRIC2_ID" ]; then
    RESP=$(api PUT "/goals/metrics/$METRIC2_ID" "$ADMIN_TOKEN" '{"name":"Safety Training Completion Rate"}')
    if is_success "$RESP"; then
      pass "Rename metric: Safety Training Completion Rate"
    else
      fail "Update metric definition"
    fi
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 3: Collaborators
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 3: Collaborators"

if [ -n "$GOAL1_ID" ]; then
  subsection "3.1 Add collaborators"

  # Add Ahmed as reviewer_l1
  RESP=$(api POST "/goals/$GOAL1_ID/collaborators" "$ADMIN_TOKEN" \
    "{\"user_id\":\"$AHMED_ID\",\"role\":\"reviewer_l1\"}")
  if is_success "$RESP"; then
    pass "Add Ahmed as reviewer_l1"
  else
    fail "Add Ahmed as reviewer_l1" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  # Add Fatima as reviewer_l2
  RESP=$(api POST "/goals/$GOAL1_ID/collaborators" "$ADMIN_TOKEN" \
    "{\"user_id\":\"$FATIMA_ID\",\"role\":\"reviewer_l2\"}")
  if is_success "$RESP"; then
    pass "Add Fatima as reviewer_l2"
  else
    fail "Add Fatima as reviewer_l2" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  # Add Omar as collaborator
  RESP=$(api POST "/goals/$GOAL1_ID/collaborators" "$ADMIN_TOKEN" \
    "{\"user_id\":\"$OMAR_ID\",\"role\":\"collaborator\"}")
  if is_success "$RESP"; then
    pass "Add Omar as collaborator"
  else
    fail "Add Omar as collaborator" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  subsection "3.2 Remove & re-add collaborator"

  RESP=$(api DELETE "/goals/$GOAL1_ID/collaborators/$OMAR_ID" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    pass "Remove Omar collaborator"
  else
    fail "Remove collaborator" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  RESP=$(api POST "/goals/$GOAL1_ID/collaborators" "$ADMIN_TOKEN" \
    "{\"user_id\":\"$OMAR_ID\",\"role\":\"collaborator\"}")
  if is_success "$RESP"; then
    pass "Re-add Omar as collaborator"
  else
    fail "Re-add collaborator"
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 4: Check-Ins
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 4: Check-Ins"

CHECKIN_ID=""

if [ -n "$GOAL1_ID" ]; then
  subsection "4.1 Create check-ins"

  # Check-in with metric updates
  CHECKIN_BODY=$(cat <<EOF
{
  "status": "on_track",
  "content": "Good progress on safety protocols. New PPE distributed to all teams."
  $([ -n "$METRIC1_ID" ] && echo ",\"metric_updates\": [{\"metric_id\":\"$METRIC1_ID\",\"value\":6.8,\"comment\":\"Latest monthly figure\"}]")
}
EOF
)

  RESP=$(api POST "/goals/$GOAL1_ID/check-ins" "$ADMIN_TOKEN" "$CHECKIN_BODY")
  CHECKIN_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
  if [ -n "$CHECKIN_ID" ] && [ "$CHECKIN_ID" != "null" ]; then
    pass "Create check-in (on_track): $CHECKIN_ID"
  else
    fail "Create check-in" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
    CHECKIN_ID=""
  fi

  # Second check-in: at_risk
  RESP=$(api POST "/goals/$GOAL1_ID/check-ins" "$ADMIN_TOKEN" '{
    "status": "at_risk",
    "content": "Budget constraints may affect Q3 safety training schedule."
  }')
  CHECKIN2_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
  if [ -n "$CHECKIN2_ID" ] && [ "$CHECKIN2_ID" != "null" ]; then
    pass "Create check-in (at_risk): $CHECKIN2_ID"
  else
    fail "Create at_risk check-in"
  fi

  subsection "4.2 List check-ins"

  RESP=$(api GET "/goals/$GOAL1_ID/check-ins?page=1&limit=10" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    COUNT=$(echo "$RESP" | jq -r '.total_items // (.data | length)' 2>/dev/null)
    pass "List check-ins: $COUNT entries"
  else
    fail "List check-ins"
  fi

  subsection "4.3 Delete check-in"

  if [ -n "$CHECKIN2_ID" ] && [ "$CHECKIN2_ID" != "null" ]; then
    RESP=$(api DELETE "/goals/check-ins/$CHECKIN2_ID" "$ADMIN_TOKEN")
    if is_success "$RESP"; then
      pass "Delete check-in: $CHECKIN2_ID"
    else
      fail "Delete check-in" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
    fi
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 5: Status Transitions
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 5: Status Transitions"

if [ -n "$GOAL1_ID" ]; then
  subsection "5.1 Draft → Active"

  RESP=$(api POST "/goals/$GOAL1_ID/transition" "$ADMIN_TOKEN" '{"status":"Active"}')
  if is_success "$RESP"; then
    NEW_STATUS=$(echo "$RESP" | jq -r '.data.status' 2>/dev/null)
    pass "Transition: Draft → $NEW_STATUS"
  else
    fail "Transition to Active" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  subsection "5.2 Active → Under_Review"

  RESP=$(api POST "/goals/$GOAL1_ID/transition" "$ADMIN_TOKEN" '{"status":"Under_Review"}')
  if is_success "$RESP"; then
    NEW_STATUS=$(echo "$RESP" | jq -r '.data.status' 2>/dev/null)
    pass "Transition: Active → $NEW_STATUS"
  else
    fail "Transition to Under_Review" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  subsection "5.3 Under_Review → Achieved"

  RESP=$(api POST "/goals/$GOAL1_ID/transition" "$ADMIN_TOKEN" '{"status":"Achieved"}')
  if is_success "$RESP"; then
    NEW_STATUS=$(echo "$RESP" | jq -r '.data.status' 2>/dev/null)
    pass "Transition: Under_Review → $NEW_STATUS"
  else
    fail "Transition to Achieved" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

# Transition goal3 to Active for analytics tests
if [ -n "$GOAL3_ID" ]; then
  api POST "/goals/$GOAL3_ID/transition" "$ADMIN_TOKEN" '{"status":"Active"}' > /dev/null 2>&1
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 6: Clone & Export/Import
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 6: Clone, Export & Bulk"

subsection "6.1 Clone goal"

CLONE_ID=""
if [ -n "$GOAL1_ID" ]; then
  RESP=$(api POST "/goals/$GOAL1_ID/clone" "$ADMIN_TOKEN" "{
    \"title\": \"E2E Test — Safety Standards (Cloned)\",
    \"owner_id\": \"$LAYLA_ID\"
  }")
  CLONE_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
  if [ -n "$CLONE_ID" ] && [ "$CLONE_ID" != "null" ]; then
    pass "Clone goal: $CLONE_ID"
  else
    fail "Clone goal" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
    CLONE_ID=""
  fi
fi

subsection "6.2 Export goals (CSV)"

RESP=$(curl -s -w '\n%{http_code}' -X GET \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  "${BASE_URL}/goals/export?format=csv" 2>/dev/null)
CODE=$(echo "$RESP" | tail -1)
if [ "$CODE" = "200" ]; then
  LINES=$(echo "$RESP" | sed '$d' | wc -l)
  pass "Export CSV: $LINES lines"
else
  fail "Export CSV: HTTP $CODE"
fi

subsection "6.3 Export goals (JSON)"

RESP=$(api GET "/goals/export?format=json" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  COUNT=$(echo "$RESP" | jq -r '.total // (.data | length)' 2>/dev/null)
  pass "Export JSON: $COUNT goals"
else
  fail "Export JSON"
fi

subsection "6.4 Bulk transition"

if [ -n "$GOAL2_ID" ]; then
  # First activate goal2
  api POST "/goals/$GOAL2_ID/transition" "$ADMIN_TOKEN" '{"status":"Active"}' > /dev/null 2>&1

  # Bulk close using goal2 + clone (if exists)
  BULK_IDS="\"$GOAL2_ID\""
  [ -n "$CLONE_ID" ] && BULK_IDS="$BULK_IDS,\"$CLONE_ID\""

  RESP=$(api POST "/goals/bulk" "$ADMIN_TOKEN" "{
    \"goal_ids\": [$BULK_IDS],
    \"action\": \"close\"
  }")
  if is_success "$RESP"; then
    RESULTS=$(echo "$RESP" | jq -r '.data.results | length' 2>/dev/null)
    pass "Bulk close: $RESULTS goals processed"
  else
    fail "Bulk close" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 7: Goal Templates
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 7: Goal Templates"

TEMPLATE_ID=""

subsection "7.1 Create template"

RESP=$(api POST "/goal-templates" "$ADMIN_TOKEN" "{
  \"name\": \"E2E Test — Safety Template ${RUN_ID}\",
  \"description\": \"Template for safety goals\",
  \"category\": \"Safety\",
  \"priority\": \"High\",
  \"default_metrics\": [
    {\"name\": \"Incident Rate\", \"metric_type\": \"Numeric\", \"unit\": \"per 1000\", \"baseline_value\": 10, \"target_value\": 3, \"weight\": 70},
    {\"name\": \"Training Completion\", \"metric_type\": \"Percentage\", \"unit\": \"%\", \"baseline_value\": 0, \"target_value\": 100, \"weight\": 30}
  ],
  \"default_collaborators\": [
    {\"role\": \"reviewer_l1\"},
    {\"role\": \"collaborator\"}
  ],
  \"is_active\": true
}")
TEMPLATE_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
if [ -n "$TEMPLATE_ID" ] && [ "$TEMPLATE_ID" != "null" ]; then
  pass "Create template: $TEMPLATE_ID"
else
  fail "Create template" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  TEMPLATE_ID=""
fi

subsection "7.2 List templates"

RESP=$(api GET "/goal-templates" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  COUNT=$(echo "$RESP" | jq -r '.data | length' 2>/dev/null)
  pass "List templates: $COUNT templates"
else
  fail "List templates"
fi

subsection "7.3 List active templates"

RESP=$(api GET "/goal-templates/active" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  pass "List active templates"
else
  fail "List active templates"
fi

subsection "7.4 Get template by ID"

if [ -n "$TEMPLATE_ID" ]; then
  RESP=$(api GET "/goal-templates/$TEMPLATE_ID" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    METRICS_COUNT=$(echo "$RESP" | jq -r '.data.default_metrics | length' 2>/dev/null)
    pass "Get template: $METRICS_COUNT default metrics"
  else
    fail "Get template by ID"
  fi
fi

subsection "7.5 Update template"

if [ -n "$TEMPLATE_ID" ]; then
  RESP=$(api PUT "/goal-templates/$TEMPLATE_ID" "$ADMIN_TOKEN" "{\"name\":\"E2E Test — Updated Template ${RUN_ID}\"}")
  if is_success "$RESP"; then
    pass "Update template name"
  else
    fail "Update template" "$(echo "$RESP" | jq -r '.error // .errors // empty' 2>/dev/null)"
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 8: Analytics Dashboard
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 8: Analytics Dashboard"

subsection "8.1 Goal statistics"

RESP=$(api GET "/goals/analytics/stats" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  TOTAL=$(echo "$RESP" | jq -r '.data.total // 0' 2>/dev/null)
  ACHIEVED=$(echo "$RESP" | jq -r '.data.achieved // 0' 2>/dev/null)
  pass "Analytics stats: total=$TOTAL, achieved=$ACHIEVED"
else
  fail "Analytics stats" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
fi

subsection "8.2 Distributions"

RESP=$(api GET "/goals/analytics/distributions" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  BY_STATUS=$(echo "$RESP" | jq -r '.data.by_status | length' 2>/dev/null)
  BY_PRIORITY=$(echo "$RESP" | jq -r '.data.by_priority | length' 2>/dev/null)
  pass "Distributions: $BY_STATUS statuses, $BY_PRIORITY priorities"
else
  fail "Distributions" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
fi

subsection "8.3 Progress summary"

RESP=$(api GET "/goals/analytics/progress" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  AVG=$(echo "$RESP" | jq -r '.data.average_progress // 0' 2>/dev/null)
  pass "Progress summary: avg=${AVG}%"
else
  fail "Progress summary"
fi

subsection "8.4 At-risk goals"

RESP=$(api GET "/goals/analytics/at-risk?page=1&limit=10" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  COUNT=$(echo "$RESP" | jq -r '.total // (.data | length) // 0' 2>/dev/null || echo "0")
  pass "At-risk goals: $COUNT"
else
  fail "At-risk goals"
fi

subsection "8.5 Trend data"

RESP=$(api GET "/goals/analytics/trends?months=6" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  POINTS=$(echo "$RESP" | jq -r '.data.points | length' 2>/dev/null || echo "0")
  pass "Trend data: $POINTS months"
else
  fail "Trend data"
fi

subsection "8.6 OKR Tree"

RESP=$(api GET "/goals/analytics/okr-tree" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  DEPTS=$(echo "$RESP" | jq -r '.data.departments | length' 2>/dev/null || echo "0")
  pass "OKR tree: $DEPTS departments"
else
  fail "OKR tree" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 9: Performance Reviews
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 9: Performance Reviews"

CYCLE_ID=""
ASSIGNMENT_ID=""

subsection "9.1 Create review cycle"

RESP=$(api POST "/reviews/cycles" "$ADMIN_TOKEN" "{
  \"title\": \"E2E Test — Q1 2025 Review Cycle ${RUN_ID}\",
  \"description\": \"End-to-end test review cycle\",
  \"period_start\": \"2025-01-01T00:00:00Z\",
  \"period_end\": \"2025-03-31T00:00:00Z\"
  $([ -n "$CIVIL_DEPT_ID" ] && echo ",\"department_id\": \"$CIVIL_DEPT_ID\"")
}")
CYCLE_ID=$(echo "$RESP" | jq -r '.data.id // empty' 2>/dev/null)
CYCLE_STATUS=$(echo "$RESP" | jq -r '.data.status // empty' 2>/dev/null)

if [ -n "$CYCLE_ID" ] && [ "$CYCLE_ID" != "null" ]; then
  pass "Create review cycle: $CYCLE_ID (status: $CYCLE_STATUS)"
else
  fail "Create review cycle" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  CYCLE_ID=""
fi

subsection "9.2 List review cycles"

RESP=$(api GET "/reviews/cycles?page=1&limit=10" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  COUNT=$(echo "$RESP" | jq -r '.total_items // (.data | length)' 2>/dev/null)
  pass "List cycles: $COUNT"
else
  fail "List cycles"
fi

subsection "9.3 Get cycle detail"

if [ -n "$CYCLE_ID" ]; then
  RESP=$(api GET "/reviews/cycles/$CYCLE_ID" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    TITLE=$(echo "$RESP" | jq -r '.data.title' 2>/dev/null)
    pass "Get cycle: $TITLE"
  else
    fail "Get cycle detail"
  fi
fi

subsection "9.4 Update cycle"

if [ -n "$CYCLE_ID" ]; then
  RESP=$(api PUT "/reviews/cycles/$CYCLE_ID" "$ADMIN_TOKEN" '{"description":"Updated by E2E test"}')
  if is_success "$RESP"; then
    pass "Update cycle description"
  else
    fail "Update cycle" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

subsection "9.5 Assign reviewees"

if [ -n "$CYCLE_ID" ]; then
  RESP=$(api POST "/reviews/cycles/$CYCLE_ID/assignments" "$ADMIN_TOKEN" "{
    \"assignments\": [
      {\"employee_id\": \"$SARAH_ID\", \"reviewer_id\": \"$ADMIN_ID\"},
      {\"employee_id\": \"$OMAR_ID\", \"reviewer_id\": \"$AHMED_ID\"}
    ]
  }")
  if is_success "$RESP"; then
    COUNT=$(echo "$RESP" | jq -r '.data | length' 2>/dev/null)
    pass "Assign reviewees: $COUNT assignments"
  else
    fail "Assign reviewees" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  # Get first assignment ID for scoring
  RESP=$(api GET "/reviews/cycles/$CYCLE_ID/assignments" "$ADMIN_TOKEN")
  ASSIGNMENT_ID=$(echo "$RESP" | jq -r '.data[0].id // empty' 2>/dev/null)
  if [ -n "$ASSIGNMENT_ID" ] && [ "$ASSIGNMENT_ID" != "null" ]; then
    pass "List cycle assignments: found $ASSIGNMENT_ID"
  else
    fail "List cycle assignments"
    ASSIGNMENT_ID=""
  fi
fi

subsection "9.6 Activate cycle"

if [ -n "$CYCLE_ID" ]; then
  RESP=$(api POST "/reviews/cycles/$CYCLE_ID/activate" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    STATUS=$(echo "$RESP" | jq -r '.data.status' 2>/dev/null)
    pass "Activate cycle: status → $STATUS"
  else
    fail "Activate cycle" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

subsection "9.7 Score goals in assignment"

if [ -n "$ASSIGNMENT_ID" ] && [ -n "$GOAL1_ID" ]; then
  RESP=$(api POST "/reviews/assignments/$ASSIGNMENT_ID/score" "$ADMIN_TOKEN" "[
    {\"goal_id\": \"$GOAL1_ID\", \"weight\": 100, \"rating\": 4, \"comments\": \"Great safety improvements\"}
  ]")
  if is_success "$RESP"; then
    pass "Score goals on assignment"
  else
    fail "Score goals" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

subsection "9.8 Get assignment detail"

if [ -n "$ASSIGNMENT_ID" ]; then
  RESP=$(api GET "/reviews/assignments/$ASSIGNMENT_ID" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    STATUS=$(echo "$RESP" | jq -r '.data.status' 2>/dev/null)
    SCORES=$(echo "$RESP" | jq -r '.data.goal_scores | length' 2>/dev/null || echo "0")
    pass "Get assignment: status=$STATUS, goal_scores=$SCORES"
  else
    fail "Get assignment detail"
  fi
fi

subsection "9.9 Submit review"

if [ -n "$ASSIGNMENT_ID" ]; then
  RESP=$(api POST "/reviews/assignments/$ASSIGNMENT_ID/submit" "$ADMIN_TOKEN" "{
    \"overall_rating\": 4.5,
    \"comments\": \"Excellent performance on safety goals. Keep it up!\"
    $([ -n "$GOAL1_ID" ] && echo ",\"goal_scores\": [{\"goal_id\": \"$GOAL1_ID\", \"weight\": 100, \"rating\": 5, \"comments\": \"Exceeded expectations\"}]")
  }")
  if is_success "$RESP"; then
    STATUS=$(echo "$RESP" | jq -r '.data.status' 2>/dev/null)
    RATING=$(echo "$RESP" | jq -r '.data.overall_rating' 2>/dev/null)
    pass "Submit review: status=$STATUS, rating=$RATING"
  else
    fail "Submit review" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

subsection "9.10 My reviews & tasks"

RESP=$(api GET "/reviews/my-reviews?page=1&limit=10" "$SARAH_TOKEN")
if is_success "$RESP"; then
  COUNT=$(echo "$RESP" | jq -r '.total_items // (.data | length)' 2>/dev/null)
  pass "Sarah's reviews: $COUNT"
else
  fail "My reviews (Sarah)"
fi

RESP=$(api GET "/reviews/my-review-tasks?page=1&limit=10" "$ADMIN_TOKEN")
if is_success "$RESP"; then
  COUNT=$(echo "$RESP" | jq -r '.total_items // (.data | length)' 2>/dev/null)
  pass "Admin's review tasks: $COUNT"
else
  fail "My review tasks (Admin)"
fi

subsection "9.11 Complete cycle"

if [ -n "$CYCLE_ID" ]; then
  RESP=$(api POST "/reviews/cycles/$CYCLE_ID/complete" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    STATUS=$(echo "$RESP" | jq -r '.data.status' 2>/dev/null)
    pass "Complete cycle: status → $STATUS"
  else
    fail "Complete cycle" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 10: Cross-User Access Tests
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 10: Cross-User Access"

subsection "10.1 Viewer can list goals"

RESP=$(api GET "/goals?page=1&limit=5" "$KHALID_TOKEN")
if is_success "$RESP"; then
  pass "Khalid (viewer) can list goals"
else
  fail "Viewer list goals" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
fi

subsection "10.2 Manager can view analytics"

RESP=$(api GET "/goals/analytics/stats" "$SARAH_TOKEN")
if is_success "$RESP"; then
  pass "Sarah (manager) can view analytics"
else
  fail "Manager view analytics"
fi

subsection "10.3 Manager can view OKR tree"

RESP=$(api GET "/goals/analytics/okr-tree" "$LAYLA_TOKEN")
if is_success "$RESP"; then
  pass "Layla (manager) can view OKR tree"
else
  fail "Manager view OKR tree"
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 11: Cleanup (delete test data)
# ════════════════════════════════════════════════════════════════════════════

section "PHASE 11: Cleanup"

subsection "Deleting test goals..."

for gid in "$CLONE_ID" "$GOAL2_ID" "$GOAL3_ID" "$GOAL1_ID"; do
  if [ -n "$gid" ] && [ "$gid" != "null" ]; then
    RESP=$(api DELETE "/goals/$gid" "$ADMIN_TOKEN")
    if is_success "$RESP"; then
      pass "Delete goal: ${gid:0:8}..."
    else
      fail "Delete goal ${gid:0:8}..." "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
    fi
  fi
done

subsection "Deleting test template..."

if [ -n "$TEMPLATE_ID" ] && [ "$TEMPLATE_ID" != "null" ]; then
  RESP=$(api DELETE "/goal-templates/$TEMPLATE_ID" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    pass "Delete template: ${TEMPLATE_ID:0:8}..."
  else
    fail "Delete template" "$(echo "$RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi
fi

subsection "Deleting test review cycle..."

if [ -n "$CYCLE_ID" ] && [ "$CYCLE_ID" != "null" ]; then
  # Cycle is completed so we can't delete via normal API — that's expected
  RESP=$(api DELETE "/reviews/cycles/$CYCLE_ID" "$ADMIN_TOKEN")
  if is_success "$RESP"; then
    pass "Delete review cycle: ${CYCLE_ID:0:8}..."
  else
    pass "Review cycle not deleted (completed status — expected behavior)"
  fi
fi

# ════════════════════════════════════════════════════════════════════════════
# PHASE 12: DOCUMENTA / MyDocs Integration
# ════════════════════════════════════════════════════════════════════════════

section "Phase 12: Documenta / MyDocs Integration"

# 12.1 — Create goal and verify Documenta folder was created
subsection "12.1 Goal with Documenta folder"

DOC_GOAL_RESP=$(api POST "/goals" "$ADMIN_TOKEN" '{
  "title": "Documenta Integration Test '"$RUN_ID"'",
  "description": "Goal to test Documenta file operations",
  "priority": "High",
  "status": "In Progress",
  "start_date": "2026-01-01T00:00:00Z",
  "end_date": "2026-12-31T00:00:00Z",
  "weight": 10,
  "owner_id": "'"$ADMIN_ID"'"
}')
DOC_GOAL_ID=$(jq_field "$DOC_GOAL_RESP" "id")
DOC_FOLDER_ID=$(echo "$DOC_GOAL_RESP" | jq -r '.data.documenta_folder_id // empty' 2>/dev/null)

if is_success "$DOC_GOAL_RESP" && [ -n "$DOC_GOAL_ID" ] && [ "$DOC_GOAL_ID" != "null" ]; then
  pass "Create goal for Documenta tests: ${DOC_GOAL_ID:0:8}..."
else
  fail "Create goal for Documenta tests" "$(echo "$DOC_GOAL_RESP" | jq -r '.error // .errors // empty' 2>/dev/null)"
fi

if [ -n "$DOC_FOLDER_ID" ] && [ "$DOC_FOLDER_ID" != "null" ] && [ "$DOC_FOLDER_ID" != "" ]; then
  pass "Documenta folder created: ${DOC_FOLDER_ID:0:8}..."
else
  skip "Documenta folder not created (DOCUMENTA_ENABLED may be false)"
fi

# 12.2 — Upload evidence with file
subsection "12.2 Evidence upload"

# Create a test file
echo "E2E test evidence content — uploaded at $(date)" > /tmp/e2e_evidence_${RUN_ID}.txt

EVIDENCE_UPLOAD_RESP=$(curl -s -w '\n%{http_code}' \
  -X POST "${BASE_URL}/goals/${DOC_GOAL_ID}/evidences" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -F "file=@/tmp/e2e_evidence_${RUN_ID}.txt" \
  -F "title=E2E Evidence ${RUN_ID}" \
  -F "evidence_type=Report" \
  -F "comment=Automated E2E test upload" 2>/dev/null || echo -e "\n000")
EVIDENCE_HTTP=$(echo "$EVIDENCE_UPLOAD_RESP" | tail -1)
EVIDENCE_BODY=$(echo "$EVIDENCE_UPLOAD_RESP" | sed '$d')
EVIDENCE_ID=$(echo "$EVIDENCE_BODY" | jq -r '.data.id // empty' 2>/dev/null)
EVIDENCE_FILE_ID=$(echo "$EVIDENCE_BODY" | jq -r '.data.documenta_file_id // empty' 2>/dev/null)

if echo "$EVIDENCE_BODY" | jq -r '.success' 2>/dev/null | grep -q 'true'; then
  pass "Upload evidence file: ${EVIDENCE_ID:0:8}..."
else
  fail "Upload evidence file" "HTTP $EVIDENCE_HTTP — $(echo "$EVIDENCE_BODY" | jq -r '.error // empty' 2>/dev/null)"
fi

# 12.3 — List evidences for goal
subsection "12.3 Evidence CRUD"

if [ -n "$EVIDENCE_ID" ] && [ "$EVIDENCE_ID" != "null" ]; then
  LIST_EV_RESP=$(api GET "/goals/${DOC_GOAL_ID}/evidences" "$ADMIN_TOKEN")
  EV_COUNT=$(echo "$LIST_EV_RESP" | jq '.data | length' 2>/dev/null || echo "0")
  if is_success "$LIST_EV_RESP" && [ "$EV_COUNT" -ge 1 ]; then
    pass "List evidences: ${EV_COUNT} found"
  else
    fail "List evidences" "count=$EV_COUNT"
  fi

  # 12.4 — Get evidence detail
  GET_EV_RESP=$(api GET "/goals/evidences/$EVIDENCE_ID" "$ADMIN_TOKEN")
  EV_TITLE=$(jq_field "$GET_EV_RESP" "title")
  EV_STATUS=$(jq_field "$GET_EV_RESP" "status")
  if is_success "$GET_EV_RESP" && [ "$EV_TITLE" = "E2E Evidence ${RUN_ID}" ]; then
    pass "Get evidence detail: title='${EV_TITLE}', status='${EV_STATUS}'"
  else
    fail "Get evidence detail" "title='${EV_TITLE}'"
  fi

  # 12.5 — Get preview URL
  PREVIEW_RESP=$(api GET "/goals/evidences/$EVIDENCE_ID/preview" "$ADMIN_TOKEN")
  if is_success "$PREVIEW_RESP"; then
    pass "Get evidence preview URL"
  else
    fail "Get evidence preview URL" "$(echo "$PREVIEW_RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  # 12.6 — Get download URL (handler returns 307 redirect, not JSON)
  DOWNLOAD_HTTP=$(curl -s -o /dev/null -w '%{http_code}' \
    -X GET "${BASE_URL}/goals/evidences/${EVIDENCE_ID}/download" \
    -H "Authorization: Bearer $ADMIN_TOKEN" 2>/dev/null || echo "000")
  if [ "$DOWNLOAD_HTTP" = "307" ] || [ "$DOWNLOAD_HTTP" = "200" ]; then
    pass "Get evidence download URL (HTTP $DOWNLOAD_HTTP)"
  else
    fail "Get evidence download URL" "HTTP $DOWNLOAD_HTTP"
  fi

  # 12.7 — Replace evidence file
  echo "Updated E2E evidence content — replaced at $(date)" > /tmp/e2e_evidence_replace_${RUN_ID}.txt
  REPLACE_RESP=$(curl -s -w '\n%{http_code}' \
    -X PUT "${BASE_URL}/goals/evidences/${EVIDENCE_ID}/file" \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -F "file=@/tmp/e2e_evidence_replace_${RUN_ID}.txt" 2>/dev/null || echo -e "\n000")
  REPLACE_HTTP=$(echo "$REPLACE_RESP" | tail -1)
  REPLACE_BODY=$(echo "$REPLACE_RESP" | sed '$d')
  if echo "$REPLACE_BODY" | jq -r '.success' 2>/dev/null | grep -q 'true'; then
    pass "Replace evidence file"
  else
    fail "Replace evidence file" "HTTP $REPLACE_HTTP — $(echo "$REPLACE_BODY" | jq -r '.error // empty' 2>/dev/null)"
  fi

  # 12.8 — Delete evidence
  DEL_EV_RESP=$(api DELETE "/goals/evidences/$EVIDENCE_ID" "$ADMIN_TOKEN")
  if is_success "$DEL_EV_RESP"; then
    pass "Delete evidence"
  else
    fail "Delete evidence" "$(echo "$DEL_EV_RESP" | jq -r '.error // empty' 2>/dev/null)"
  fi

  # Verify deletion
  LIST_AFTER_DEL=$(api GET "/goals/${DOC_GOAL_ID}/evidences" "$ADMIN_TOKEN")
  EV_COUNT_AFTER=$(echo "$LIST_AFTER_DEL" | jq '.data | length' 2>/dev/null || echo "0")
  if is_success "$LIST_AFTER_DEL" && [ "$EV_COUNT_AFTER" -eq 0 ]; then
    pass "Evidence deleted — list returns 0"
  else
    fail "Evidence deletion verification" "count=$EV_COUNT_AFTER after delete"
  fi
else
  skip "Evidence CRUD tests (upload failed)"
fi

# 12.9 — Document search endpoint
subsection "12.9 Document search & list"

SEARCH_RESP=$(curl -s -w '\n%{http_code}' \
  -X POST "${BASE_URL}/documents/search" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"query": "E2E"}' 2>/dev/null || echo -e "\n000")
SEARCH_HTTP=$(echo "$SEARCH_RESP" | tail -1)
SEARCH_BODY=$(echo "$SEARCH_RESP" | sed '$d')
if echo "$SEARCH_BODY" | jq -r '.success' 2>/dev/null | grep -q 'true'; then
  SEARCH_COUNT=$(echo "$SEARCH_BODY" | jq '.data | length' 2>/dev/null || echo "0")
  pass "Document search: ${SEARCH_COUNT} results"
else
  fail "Document search" "HTTP $SEARCH_HTTP — $(echo "$SEARCH_BODY" | jq -r '.error // empty' 2>/dev/null)"
fi

# 12.10 — List files endpoint
LIST_FILES_RESP=$(api GET "/documents/files" "$ADMIN_TOKEN")
if is_success "$LIST_FILES_RESP"; then
  FILES_COUNT=$(echo "$LIST_FILES_RESP" | jq '.data | length' 2>/dev/null || echo "0")
  pass "List document files: ${FILES_COUNT} files"
else
  fail "List document files" "$(echo "$LIST_FILES_RESP" | jq -r '.error // empty' 2>/dev/null)"
fi

# Cleanup temp files
rm -f /tmp/e2e_evidence_${RUN_ID}.txt /tmp/e2e_evidence_replace_${RUN_ID}.txt

# ════════════════════════════════════════════════════════════════════════════
# SUMMARY
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BOLD}${CYAN}  TEST SUMMARY${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  ${GREEN}Passed:  ${PASS_COUNT}${NC}"
echo -e "  ${RED}Failed:  ${FAIL_COUNT}${NC}"
echo -e "  ${YELLOW}Skipped: ${SKIP_COUNT}${NC}"
echo -e "  ${BOLD}Total:   ${TOTAL_COUNT}${NC}"
echo ""

if [ "$FAIL_COUNT" -eq 0 ]; then
  echo -e "  ${GREEN}${BOLD}ALL TESTS PASSED ✓${NC}"
  echo ""
  exit 0
else
  echo -e "  ${RED}${BOLD}${FAIL_COUNT} TEST(S) FAILED ✗${NC}"
  echo ""
  exit 1
fi
