//go:build windows

package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsSecretFilePermissionsRejectBroadRead(t *testing.T) {
	secretPath := createWindowsSecretFixture(t)
	setWindowsSecretACL(t, secretPath)
	runICACLS(t, secretPath, "/grant", "*S-1-5-32-545:(R)")
	t.Cleanup(func() {
		runICACLS(t, secretPath, "/remove:g", "*S-1-5-32-545")
	})

	if _, err := (SecretSource{File: secretPath}).Resolve(nil); err == nil || !strings.Contains(err.Error(), secretPath) {
		t.Fatalf("Resolve() error = %v, want path-scoped ACL rejection", err)
	}
}

func TestWindowsSecretFilePermissionsAllowOwnerSystemAndAdministrators(t *testing.T) {
	secretPath := createWindowsSecretFixture(t)
	setWindowsSecretACL(t, secretPath)

	secret, err := (SecretSource{File: secretPath}).Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if secret != "secret-value" {
		t.Fatalf("Resolve() = %q", secret)
	}
}

func createWindowsSecretFixture(t *testing.T) string {
	t.Helper()
	secretPath := filepath.Join(t.TempDir(), "hosted.secret")
	if err := os.WriteFile(secretPath, []byte("secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return secretPath
}

func setWindowsSecretACL(t *testing.T, secretPath string) {
	t.Helper()
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatalf("read current user SID: %v", err)
	}
	runICACLS(t, secretPath, "/inheritance:r", "/grant:r",
		"*"+user.User.Sid.String()+":(F)", "*S-1-5-18:(F)", "*S-1-5-32-544:(F)")
	// Elevated runners can create files owned by Administrators. The fixture
	// grants the process user access, so that user must also be its owner.
	runICACLS(t, secretPath, "/setowner", "*"+user.User.Sid.String())
}

func secureSecretFixturePermissions(t *testing.T, secretPath string) {
	t.Helper()
	setWindowsSecretACL(t, secretPath)
}

func runICACLS(t *testing.T, arguments ...string) {
	t.Helper()
	command := exec.Command("icacls.exe", arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("icacls %v: %v\n%s", arguments, err, output)
	}
}
