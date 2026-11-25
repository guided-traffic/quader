// Package devprofile holds the tests of the development profiles' scripts that hand secrets from the developer's machine to Quader.
// deploy/dev/upstream-accounts.sh (Q-ARCH-1066, Q-ARCH-1067) runs with bash -x against an account file of made-up values in a
// temporary directory, with kubectl stubbed: the files it writes and their modes, the Docker configuration it prints, the kubectl
// calls it makes, the one line it says without an account, the rule that a git worktree reads no account unless a variable names
// a file, and that neither its trace, its output nor a command line it runs shows a value. No cluster, no Docker and no network.
package devprofile
