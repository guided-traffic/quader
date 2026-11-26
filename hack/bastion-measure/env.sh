# shellcheck shell=bash
# The names, ports and paths of the measurement W34-E4; sourced by setup.sh and run.sh after deploy/ui-dev/instance.sh's
# ui_instance. The ports are the instance's scanner ports (19082 to 19089 plus the instance's step), which the measurement's
# main installation does not use: it runs no scanner.
off=$(((UI_INSTANCE - 1) * 100))
E4="$UI_STATE/e4"
E4_MAIN_PORT=$UI_BACKEND_PORT
E4_MAIN_OPS_PORT=$UI_OPS_PORT
E4_BASTION_PORT=$((19086 + off))
E4_BASTION_OPS_PORT=$((19087 + off))
E4_STUB_PORT=$((19088 + off))
# The Bastion's host as the client containers name it; --add-host maps it to the host gateway.
E4_HOST=packages.e4.test
E4_STUB_HOST=stub.e4.test
E4_REG=e4-bastion
E4_PROJECT=e4
export E4 E4_MAIN_PORT E4_MAIN_OPS_PORT E4_BASTION_PORT E4_BASTION_OPS_PORT E4_STUB_PORT E4_HOST E4_STUB_HOST E4_REG E4_PROJECT
