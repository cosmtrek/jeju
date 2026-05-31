package cli

import "testing"

func TestValidateWorkspaceCommandAllowsBenchmarkCommands(t *testing.T) {
	commands := []string{
		"git -C repo status --porcelain",
		"python3 check.py",
		"mkdir -p ssl && openssl req -x509 -newkey rsa:2048 -nodes -keyout ssl/server.key -out ssl/server.crt -days 365 -subj '/O=DevOps Team/CN=dev-internal.company.local'",
		"cat ssl/server.key ssl/server.crt > ssl/server.pem",
	}
	for _, command := range commands {
		if err := validateWorkspaceCommand(command); err != nil {
			t.Fatalf("expected command to be allowed %q: %v", command, err)
		}
	}
}

func TestValidateWorkspaceCommandRejectsWorkspaceEscapes(t *testing.T) {
	commands := []string{
		"cd .. && git status",
		"rm -rf ../repo",
		"git -C /workspace/jeju status",
		"python3 /app/check.py",
		"cat ~/secret",
		"python3 check.py 2>../err.log",
		"echo $(pwd)",
		"echo `pwd`",
	}
	for _, command := range commands {
		if err := validateWorkspaceCommand(command); err == nil {
			t.Fatalf("expected command to be rejected: %q", command)
		}
	}
}
