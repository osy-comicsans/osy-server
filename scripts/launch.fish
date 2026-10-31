#!/usr/bin/env fish

set -g SCRIPT_DIR (cd (dirname (status filename)); and pwd)
set -g ROOT (realpath "$SCRIPT_DIR/..")
cd $ROOT

set -g BOLD (set_color --bold)
set -g DIM (set_color --dim)
set -g GREEN (set_color green)
set -g YELLOW (set_color yellow)
set -g CYAN (set_color cyan)
set -g RED (set_color red)
set -g RESET (set_color normal)

set -g STAMP (date +%Y%m%d-%H%M%S)
set -g LOG_DIR "$ROOT/logs"
set -g LOG_FILE "$LOG_DIR/osyn-$STAMP.log"

function env_value
    set -l key $argv[1]
    set -l line (grep -E "^$key=" "$ROOT/.env" 2>/dev/null | tail -n 1)
    if test -z "$line"
        return 0
    end
    set -l pair (string split -m 1 '=' -- $line)
    if test (count $pair) -ge 2
        printf '%s\n' $pair[2]
    end
end

function banner
    printf '%s\n' "$CYAN$BOLD"
    printf '%s\n' "  +--------------------------------------------------------------+"
    printf '%s\n' "  |                    OSYN  /  SMS CONTROL PLANE                |"
    printf '%s\n' "  |       Africa's Talking  ->  PostgreSQL  ->  NATS  ->  Worker |"
    printf '%s\n' "  +--------------------------------------------------------------+"
    printf '%s\n' "$RESET"
end

function step
    printf '%s[%s]%s %s\n' "$GREEN" "$argv[1]" "$RESET" "$argv[2]"
end

function warn
    printf '%s[WARN]%s %s\n' "$YELLOW" "$RESET" "$argv[1]"
end

function fail
    printf '%s[FAIL]%s %s\n' "$RED" "$RESET" "$argv[1]" >&2
    exit 1
end

function require_environment
    command -sq docker; or fail "docker is not installed"
    docker compose version >/dev/null 2>&1; or fail "docker compose is not available"
    test -f "$ROOT/.env"; or fail "missing .env; run: cp .env.example .env"
    set -g API_PORT (env_value API_HOST_PORT)
    if test -z "$API_PORT"
        set -g API_PORT 8080
    end
end

function wait_for_readiness
    set -l attempts $argv[1]
    for i in (seq 1 $attempts)
        set -l response (curl -fsS --max-time 3 "http://localhost:$API_PORT/ready" 2>/dev/null; or true)
        if string match -q '*"status":"ready"*' -- "$response"
            printf '%s\n' "$response"
            return 0
        end
        sleep 2
    end
    return 1
end

function save_logs
    mkdir -p "$LOG_DIR"
    docker compose logs --no-color --timestamps >"$LOG_FILE"
end

function print_summary
    set -l ready (curl -fsS --max-time 5 "http://localhost:$API_PORT/ready")
    set -l telegram_enabled (env_value TELEGRAM_NOTIFICATIONS_ENABLED)
    set -l telegram_dry_run (env_value TELEGRAM_DRY_RUN)
    set -l twilio_dry_run (env_value TWILIO_DRY_RUN)
    if test -z "$telegram_enabled"
        set telegram_enabled false
    end
    if test -z "$telegram_dry_run"
        set telegram_dry_run true
    end
    if test -z "$twilio_dry_run"
        set twilio_dry_run true
    end
    printf '\n%s%sOSYN IS READY%s\n\n' "$GREEN" "$BOLD" "$RESET"
    printf '  %-12s %s\n' "API" "http://localhost:$API_PORT"
    printf '  %-12s %s\n' "Swagger" "http://localhost:$API_PORT/swagger/index.html"
    printf '  %-12s %s\n' "Readiness" "$ready"
    printf '  %-12s %s\n' "Logs" "$LOG_FILE"
    printf '  %-12s %s\n' "Telegram" "enabled=$telegram_enabled dry_run=$telegram_dry_run"
    printf '  %-12s %s\n' "Twilio" "dry_run=$twilio_dry_run"
    printf '\n'
    docker compose ps
    printf '\n%sFollow live logs:%s ./scripts/launch.fish logs\n' "$DIM" "$RESET"
    printf '%sStop the stack:%s   ./scripts/launch.fish stop\n\n' "$DIM" "$RESET"
end

function launch
    banner
    require_environment
    step "1/4" "Building and starting PostgreSQL, NATS, API, fetcher, publisher, worker"
    docker compose up -d --build
    step "2/4" "Waiting for readiness on port $API_PORT"
    if not wait_for_readiness 45
        docker compose logs --tail=80
        fail "OSYN did not become ready"
    end
    step "3/4" "Capturing timestamped logs"
    save_logs
    step "4/4" "Demo environment online"
    print_summary
end

function show_status
    require_environment
    banner
    docker compose ps
    curl -fsS "http://localhost:$API_PORT/health"; or true
    printf '\n'
    curl -fsS "http://localhost:$API_PORT/ready"; or true
    printf '\n'
end

function health
    require_environment
    printf '%sHealth:%s ' "$BOLD" "$RESET"
    curl -fsS "http://localhost:$API_PORT/health"
    printf '\n%sReadiness:%s ' "$BOLD" "$RESET"
    curl -fsS "http://localhost:$API_PORT/ready"
    printf '\n'
end

function logs
    require_environment
    mkdir -p "$LOG_DIR"
    printf '%sStreaming logs to:%s %s\n' "$CYAN" "$RESET" "$LOG_FILE"
    docker compose logs -f --tail=100 --no-color | tee -a "$LOG_FILE"
end

function save
    require_environment
    save_logs
    printf '%sSaved logs:%s %s\n' "$CYAN" "$RESET" "$LOG_FILE"
end

function stop
    require_environment
    docker compose down
end

function restart
    require_environment
    docker compose down
    launch
end

function usage
    banner
    printf '%s\n' "Usage: ./scripts/launch.fish <command>"
    printf '%s\n' ""
    printf '%s\n' "Commands:"
    printf '%s\n' "  launch     Build, start, wait for readiness, save logs, and show demo summary"
    printf '%s\n' "  status     Show container status, health, and readiness"
    printf '%s\n' "  health     Print health and readiness responses"
    printf '%s\n' "  logs       Follow live logs and append them to a timestamped file"
    printf '%s\n' "  save       Save a timestamped snapshot of all logs"
    printf '%s\n' "  restart    Stop the stack and launch it again"
    printf '%s\n' "  stop       Stop all services"
    printf '%s\n' ""
    printf '%s\n' "Default command: launch"
end

set -g command $argv[1]
if test -z "$command"
    set command launch
end

switch $command
    case launch start up
        launch
    case status ps
        show_status
    case health
        health
    case logs follow
        logs
    case save snapshot
        save
    case restart
        restart
    case stop down
        stop
    case help --help -h
        usage
    case '*'
        usage
        fail "unknown command: $command"
end
