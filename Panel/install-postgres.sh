#!/usr/bin/env bash
# Installs and prepares PostgreSQL for the CorvusMiner panel on Linux.
#
#   * installs postgresql + contrib if missing (apt/dnf/yum/pacman/zypper/apk)
#   * starts and enables the service
#   * sets a password for the postgres role (default: corvus)
#   * creates the panel database (default: corvus)
#   * prints the DATABASE_URL and how to change the password
#
# Usage:
#   ./install-postgres.sh                          # defaults: db/user corvus...
#   ./install-postgres.sh --password 's3cret'
#   ./install-postgres.sh --db corvus --user postgres --password corvus
#   ./install-postgres.sh --check                  # report only, change nothing
#   ./install-postgres.sh --help
set -euo pipefail

DB_NAME="corvus"
DB_USER="postgres"
DB_PASSWORD="corvus"
PORT="5432"
HOST="127.0.0.1"
CHECK_ONLY=0

# ── Output helpers ───────────────────────────────────────────────────────────
if [ -t 1 ]; then
    C_INFO=$'\033[36m'; C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_ERR=$'\033[31m'
    C_BOLD=$'\033[1m'; C_OFF=$'\033[0m'
else
    C_INFO=''; C_OK=''; C_WARN=''; C_ERR=''; C_BOLD=''; C_OFF=''
fi

info() { printf '%s[INFO]%s %s\n' "$C_INFO" "$C_OFF" "$*"; }
ok()   { printf '%s[ OK ]%s %s\n' "$C_OK" "$C_OFF" "$*"; }
warn() { printf '%s[WARN]%s %s\n' "$C_WARN" "$C_OFF" "$*" >&2; }
fail() { printf '%s[FAIL]%s %s\n' "$C_ERR" "$C_OFF" "$*" >&2; }

usage() {
    awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"
    exit 0
}

while [ "$#" -gt 0 ]; do
    if [ "$#" -lt 2 ]; then
        case "$1" in
            --check|-h|--help) : ;;
            *) fail "Missing value for $1"; exit 2 ;;
        esac
    fi
    case "$1" in
        --password|-p) DB_PASSWORD="${2:-}"; shift 2 ;;
        --user|-u)     DB_USER="${2:-}";     shift 2 ;;
        --db|-d)       DB_NAME="${2:-}";     shift 2 ;;
        --port)        PORT="${2:-}";        shift 2 ;;
        --host)        HOST="${2:-}";        shift 2 ;;
        --check)       CHECK_ONLY=1;         shift ;;
        -h|--help)     usage ;;
        *)
            echo "Unknown option: $1" >&2
            echo "Try --help" >&2
            exit 2
            ;;
    esac
done

# ── Validation ───────────────────────────────────────────────────────────────
# The password and identifiers end up inside SQL, so keep them to a safe set
# instead of trying to escape arbitrarily.
case "$DB_PASSWORD" in
    '') fail "Password cannot be empty."; exit 2 ;;
    *[!A-Za-z0-9_.@#%+-]*) fail "Password may only contain A-Z a-z 0-9 _ . @ # % + -"; exit 2 ;;
esac
case "$DB_NAME" in
    ''|*[!A-Za-z0-9_]*) fail "Database name may only contain A-Z a-z 0-9 _"; exit 2 ;;
esac
case "$DB_USER" in
    ''|*[!A-Za-z0-9_]*) fail "Role name may only contain A-Z a-z 0-9 _"; exit 2 ;;
esac
case "$PORT" in
    ''|*[!0-9]*) fail "Port must be numeric."; exit 2 ;;
esac

# ── Privilege helper ─────────────────────────────────────────────────────────
if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
    root() { "$@"; }
    require_root() { :; }
elif command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
    root() { sudo "$@"; }
    # Deferred so --check stays read-only and never prompts.
    require_root() {
        if ! sudo -v 2>/dev/null; then
            fail "This script needs root privileges."
            printf 'Re-run it with: sudo %s\n' "$0" >&2
            exit 1
        fi
    }
else
    SUDO=""
    root() { "$@"; }
    require_root() { fail "This script needs root (or sudo)."; exit 1; }
fi

as_postgres() {
    if [ "$(id -u)" -eq 0 ]; then
        su -s /bin/sh postgres -c "$(printf '%q ' "$@")"
    else
        sudo -u postgres "$@"
    fi
}

psql_super() {
    as_postgres psql -v ON_ERROR_STOP=1 -p "$PORT" -d postgres -tAc "$1"
}

# Same, but never prompts for a password; used by the read-only report so
# --check cannot hang on a sudo prompt.
psql_probe() {
    if [ "$(id -u)" -eq 0 ]; then
        psql_super "$1"
    elif sudo -n true >/dev/null 2>&1; then
        psql_super "$1"
    else
        return 1
    fi
}

# ── Status ───────────────────────────────────────────────────────────────────
server_ready() {
    command -v pg_isready >/dev/null 2>&1 && pg_isready -h "$HOST" -p "$PORT" >/dev/null 2>&1
}

service_state() {
    if command -v systemctl >/dev/null 2>&1; then
        if systemctl is-active --quiet postgresql 2>/dev/null; then echo "active"; else echo "inactive"; fi
    elif command -v rc-service >/dev/null 2>&1; then
        if rc-service postgresql status >/dev/null 2>&1; then echo "active"; else echo "inactive"; fi
    else
        echo "unknown"
    fi
}

report() {
    echo
    info "PostgreSQL status"
    printf '  %-22s %s\n' "psql" "$(command -v psql || echo 'not installed')"
    printf '  %-22s %s\n' "server" "$(service_state)"
    printf '  %-22s %s\n' "accepting at $HOST:$PORT" "$(server_ready && echo yes || echo no)"
    if server_ready; then
        local ver
        ver="$(psql_probe 'show server_version' 2>/dev/null || true)"
        [ -z "$ver" ] && ver="$(psql --version 2>/dev/null | awk '{print $NF}' | tr -d ')') (client)"
        printf '  %-22s %s\n' "version" "$ver"

        local exists
        if exists="$(psql_probe "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME'" 2>/dev/null)"; then
            printf '  %-22s %s\n' "database '$DB_NAME'" "$([ "$exists" = "1" ] && echo exists || echo missing)"
        else
            printf '  %-22s %s\n' "database '$DB_NAME'" "unknown (needs root)"
        fi
    fi
    echo
}

if [ "$CHECK_ONLY" -eq 1 ]; then
    report
    # Non-zero when the server is not usable, so the check can gate automation.
    if server_ready; then exit 0; fi
    exit 1
fi

# ── Install ──────────────────────────────────────────────────────────────────
require_root

if command -v psql >/dev/null 2>&1 && command -v pg_isready >/dev/null 2>&1; then
    ok "PostgreSQL client tools already installed."
else
    if command -v apt-get >/dev/null 2>&1; then
        PM=apt; PKGS=(postgresql postgresql-contrib)
    elif command -v dnf >/dev/null 2>&1; then
        PM=dnf; PKGS=(postgresql-server postgresql-contrib)
    elif command -v yum >/dev/null 2>&1; then
        PM=yum; PKGS=(postgresql-server postgresql-contrib)
    elif command -v pacman >/dev/null 2>&1; then
        PM=pacman; PKGS=(postgresql)
    elif command -v zypper >/dev/null 2>&1; then
        PM=zypper; PKGS=(postgresql postgresql-contrib)
    elif command -v apk >/dev/null 2>&1; then
        PM=apk; PKGS=(postgresql postgresql-contrib)
    else
        fail "No supported package manager found (apt, dnf, yum, pacman, zypper, apk)."
        exit 1
    fi

    info "Installing ${PKGS[*]} with $PM"
    case "$PM" in
        apt)
            root apt-get update -qq
            DEBIAN_FRONTEND=noninteractive root apt-get install -y --no-install-recommends "${PKGS[@]}"
            ;;
        dnf|yum) root "$PM" install -y "${PKGS[@]}" ;;
        pacman) root pacman -Sy --needed --noconfirm "${PKGS[@]}" ;;
        zypper) root zypper --non-interactive install "${PKGS[@]}" ;;
        apk) root apk add --no-cache "${PKGS[@]}" ;;
    esac
    ok "PostgreSQL packages installed."
fi

# ── Initialise + start ───────────────────────────────────────────────────────
if ! server_ready; then
    if command -v systemctl >/dev/null 2>&1; then
        # Arch ships an empty data directory; Debian/RHEL initialise on install.
        if [ ! -s /var/lib/postgres/data/PG_VERSION ] && [ -d /var/lib/postgres ] && ! [ -d /var/lib/postgresql ]; then
            info "Initialising the Arch data directory"
            root mkdir -p /var/lib/postgres/data
            root chown -R postgres:postgres /var/lib/postgres
            as_postgres initdb -D /var/lib/postgres/data >/dev/null
        fi
        info "Starting PostgreSQL"
        if ! root systemctl enable --now postgresql >/dev/null 2>&1; then
            # RHEL/Fedora use a versioned unit and need initdb first.
            if command -v postgresql-setup >/dev/null 2>&1; then
                root postgresql-setup --initdb >/dev/null 2>&1 || true
            fi
            root systemctl enable --now postgresql-server >/dev/null 2>&1 || true
        fi
    elif command -v rc-service >/dev/null 2>&1; then
        if [ ! -s /var/lib/postgresql/data/PG_VERSION ] && [ -x /etc/init.d/postgresql ]; then
            root /etc/init.d/postgresql setup >/dev/null 2>&1 || true
        fi
        info "Starting PostgreSQL (OpenRC)"
        root rc-update add postgresql default >/dev/null 2>&1 || true
        root rc-service postgresql start >/dev/null 2>&1 || true
    else
        warn "No service manager found; start PostgreSQL manually."
    fi
fi

# Wait for the server to accept connections.
for _ in $(seq 1 30); do
    server_ready && break
    sleep 1
done

if ! server_ready; then
    fail "PostgreSQL is not accepting connections at $HOST:$PORT."
    warn "Check the service: systemctl status postgresql   (or: journalctl -u postgresql)"
    exit 1
fi
ok "PostgreSQL is accepting connections at $HOST:$PORT."

# ── Credentials + database ───────────────────────────────────────────────────
if [ "$(psql_super "SELECT 1 FROM pg_roles WHERE rolname = '$DB_USER'")" = "1" ]; then
    info "Setting the password for role '$DB_USER'"
    psql_super "ALTER ROLE \"$DB_USER\" WITH LOGIN PASSWORD '$DB_PASSWORD';" >/dev/null
    ok "Password set."
else
    info "Creating role '$DB_USER'"
    psql_super "CREATE ROLE \"$DB_USER\" WITH LOGIN PASSWORD '$DB_PASSWORD';" >/dev/null
    ok "Role created."
fi

existing_db="$(psql_super "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME'")"
if [ "$existing_db" = "1" ]; then
    ok "Database '$DB_NAME' already exists."
else
    info "Creating database '$DB_NAME' owned by '$DB_USER'"
    psql_super "CREATE DATABASE \"$DB_NAME\" OWNER \"$DB_USER\";" >/dev/null
    ok "Database created."
fi

# ── Summary ──────────────────────────────────────────────────────────────────
DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${HOST}:${PORT}/${DB_NAME}?sslmode=disable"

cat <<EOF

${C_BOLD}============================================================${C_OFF}
${C_BOLD} PostgreSQL is ready for the CorvusMiner panel${C_OFF}
${C_BOLD}============================================================${C_OFF}
 Host      : ${HOST}
 Port      : ${PORT}
 Database  : ${DB_NAME}
 User      : ${DB_USER}
 Password  : ${DB_PASSWORD}

 Export this before starting the panel:

   export DATABASE_URL="${DATABASE_URL}"

 Then run:

   cd Panel && ./panel

 Change the password later:

   sudo -u postgres psql -c "ALTER USER ${DB_USER} PASSWORD 'a-new-password';"

 Verify the login works:

   PGPASSWORD='${DB_PASSWORD}' psql -h ${HOST} -p ${PORT} -U ${DB_USER} -d ${DB_NAME} -c 'select 1;'
${C_BOLD}============================================================${C_OFF}
EOF

if [ "$DB_PASSWORD" = "corvus" ]; then
    warn "You are using the default password 'corvus'. Change it before exposing this"
    warn "database beyond localhost:  sudo -u postgres psql -c \"ALTER USER ${DB_USER} PASSWORD '...';\""
fi
case "$HOST" in
    127.0.0.1|localhost|::1) : ;;
    *)
        warn "PostgreSQL only listens on localhost by default. To reach it via '$HOST',"
        warn "set listen_addresses in postgresql.conf and allow the network in pg_hba.conf."
        ;;
esac
