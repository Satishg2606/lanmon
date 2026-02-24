// Package sshpush handles SSH key distribution to remote hosts.
package sshpush

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// PushKey connects to the target host via SSH with password authentication,
// appends the server's public key to the target user's authorized_keys,
// and verifies passwordless authentication works.
func PushKey(host string, port int, user, password, pubKeyPath, knownHostsPath string) error {
	// Read the local public key
	pubKeyData, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("reading public key %s: %w", pubKeyPath, err)
	}
	pubKey := strings.TrimSpace(string(pubKeyData))

	// Setup host key callback
	hostKeyCallback, err := getHostKeyCallback(knownHostsPath)
	if err != nil {
		return fmt.Errorf("setting up host key verification: %w", err)
	}

	// Connect with password auth
	addr := fmt.Sprintf("%s:%d", host, port)
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("SSH dial to %s: %w", addr, err)
	}
	defer client.Close()

	// Build the remote command to inject the key
	homeDir := fmt.Sprintf("/home/%s", user)
	if user == "root" {
		homeDir = "/root"
	}

	sshDir := fmt.Sprintf("%s/.ssh", homeDir)
	authKeysFile := fmt.Sprintf("%s/authorized_keys", sshDir)

	// Check for duplicate key before appending
	cmd := fmt.Sprintf(
		`mkdir -p %s && chmod 700 %s && `+
			`(grep -qF '%s' %s 2>/dev/null && echo 'KEY_EXISTS' || `+
			`(echo '%s' >> %s && chmod 600 %s && chown -R %s:%s %s && echo 'KEY_ADDED'))`,
		sshDir, sshDir,
		pubKey, authKeysFile,
		pubKey, authKeysFile, authKeysFile,
		user, user, sshDir,
	)

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("remote command failed: %w\nOutput: %s", err, string(output))
	}

	result := strings.TrimSpace(string(output))
	if result == "KEY_EXISTS" {
		return fmt.Errorf("public key already exists in %s", authKeysFile)
	}
	if result != "KEY_ADDED" {
		return fmt.Errorf("unexpected output from remote command: %s", result)
	}

	// Verify passwordless auth works
	if err := verifyPubKeyAuth(addr, user, pubKeyPath, hostKeyCallback); err != nil {
		return fmt.Errorf("verification failed — key was pushed but pubkey auth did not work: %w", err)
	}

	return nil
}

// verifyPubKeyAuth attempts to connect using public key authentication
// and runs 'echo OK' to verify the setup works.
func verifyPubKeyAuth(addr, user, pubKeyPath string, hostKeyCallback ssh.HostKeyCallback) error {
	// Derive private key path from public key path
	privKeyPath := strings.TrimSuffix(pubKeyPath, ".pub")

	privKeyData, err := os.ReadFile(privKeyPath)
	if err != nil {
		return fmt.Errorf("reading private key %s: %w", privKeyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(privKeyData)
	if err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("pubkey auth dial: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating verification session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput("echo OK")
	if err != nil {
		return fmt.Errorf("verification command failed: %w", err)
	}

	if strings.TrimSpace(string(output)) != "OK" {
		return fmt.Errorf("unexpected verification output: %s", string(output))
	}

	return nil
}

// getHostKeyCallback returns an SSH host key callback.
// If the known_hosts file exists, it uses strict checking.
// Otherwise, it uses an accept-all callback (with a warning).
func getHostKeyCallback(knownHostsPath string) (ssh.HostKeyCallback, error) {
	if knownHostsPath == "" {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	// If the file doesn't exist, create it and use a TOFU (Trust On First Use) callback
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		// Create the known_hosts file
		if err := os.MkdirAll(strings.TrimSuffix(knownHostsPath, "/known_hosts"), 0700); err != nil {
			return nil, fmt.Errorf("creating known_hosts directory: %w", err)
		}
		f, err := os.Create(knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("creating known_hosts file: %w", err)
		}
		f.Close()

		// Return a TOFU callback that records the key
		return tofuCallback(knownHostsPath), nil
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("loading known_hosts: %w", err)
	}
	return wrapKnownHostsCallback(callback, knownHostsPath), nil
}

// tofuCallback returns a callback that accepts any host key and saves it
// to the known_hosts file (Trust On First Use).
func tofuCallback(knownHostsPath string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		line := knownhosts.Line([]string{knownhosts.Normalize(remote.String())}, key)
		f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return fmt.Errorf("opening known_hosts for writing: %w", err)
		}
		defer f.Close()
		_, err = fmt.Fprintln(f, line)
		return err
	}
}

// wrapKnownHostsCallback wraps the strict knownhosts callback to handle
// unknown hosts by adding them (TOFU for new hosts).
func wrapKnownHostsCallback(callback ssh.HostKeyCallback, knownHostsPath string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		if err == nil {
			return nil
		}

		// If the error is a key-not-found error, add the key (TOFU)
		var keyErr *knownhosts.KeyError
		if isKeyError(err, &keyErr) && len(keyErr.Want) == 0 {
			// No existing key — this is a new host, add it
			line := knownhosts.Line([]string{knownhosts.Normalize(remote.String())}, key)
			f, ferr := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
			if ferr != nil {
				return fmt.Errorf("opening known_hosts for writing: %w", ferr)
			}
			defer f.Close()
			if _, ferr = fmt.Fprintln(f, line); ferr != nil {
				return ferr
			}
			return nil
		}

		// Key mismatch — this is a potential MITM warning
		return err
	}
}

// isKeyError checks if the error is a knownhosts.KeyError.
func isKeyError(err error, target **knownhosts.KeyError) bool {
	if ke, ok := err.(*knownhosts.KeyError); ok {
		*target = ke
		return true
	}
	return false
}
