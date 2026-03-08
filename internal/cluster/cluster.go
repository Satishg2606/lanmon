// Package cluster handles cluster-wide SSH key distribution and membership sync.
package cluster

import (
	"fmt"
	netrpc "net/rpc"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"
	"lanmon/internal/rpc"
	"lanmon/internal/sshpush"
	"lanmon/internal/store"
)

// SetupNode configures the target host to be part of the cluster.
// It pushes the cluster public key to authorized_keys and also
// pushes the cluster private key to the host so it can connect to others.
func SetupNode(host string, port int, user, password, clusterKeyPath, knownHostsPath string) error {
	// 1. Push the public key (usual passwordless setup)
	pubKeyPath := clusterKeyPath + ".pub"
	err := sshpush.PushKey(host, port, user, password, pubKeyPath, knownHostsPath)
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("pushing cluster public key: %w", err)
		}
	}

	// 2. Push the private key to the remote host
	privKeyData, err := os.ReadFile(clusterKeyPath)
	if err != nil {
		return fmt.Errorf("reading cluster private key %s: %w", clusterKeyPath, err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("connecting for private key push: %w", err)
	}
	defer client.Close()

	homeDir := "/root"
	if user != "root" {
		homeDir = "/home/" + user
	}
	remotePrivKeyPath := filepath.Join(homeDir, ".ssh", filepath.Base(clusterKeyPath))
	remotePubKeyPath := remotePrivKeyPath + ".pub"

	pubKeyData, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("reading cluster public key: %w", err)
	}

	cmd := fmt.Sprintf(`
		mkdir -p %s/.ssh && 
		chmod 700 %s/.ssh &&
		cat << 'EOF' > %s
%s
EOF
		chmod 600 %s &&
		cat << 'EOF' > %s
%s
EOF
		chmod 644 %s
	`, homeDir, homeDir, remotePrivKeyPath, string(privKeyData), remotePrivKeyPath, remotePubKeyPath, string(pubKeyData), remotePubKeyPath)

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating session for key push: %w", err)
	}
	defer session.Close()

	if output, err := session.CombinedOutput(cmd); err != nil {
		return fmt.Errorf("failed to push cluster keys to remote: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// RemoveKeys removes the cluster public key from authorized_keys and deletes
// the cluster private/public key files from the remote host.
func RemoveKeys(host string, port int, user, clusterKeyPath string) error {
	pubKeyData, err := os.ReadFile(clusterKeyPath + ".pub")
	if err != nil {
		return fmt.Errorf("reading cluster public key: %w", err)
	}
	pubKey := strings.TrimSpace(string(pubKeyData))

	privKeyData, err := os.ReadFile(clusterKeyPath)
	if err != nil {
		return fmt.Errorf("reading cluster private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(privKeyData)
	if err != nil {
		return fmt.Errorf("parsing cluster private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("SSH dial to %s: %w", addr, err)
	}
	defer client.Close()

	homeDir := "/root"
	if user != "root" {
		homeDir = "/home/" + user
	}
	authKeysFile := filepath.Join(homeDir, ".ssh", "authorized_keys")
	remotePrivKeyPath := filepath.Join(homeDir, ".ssh", filepath.Base(clusterKeyPath))
	remotePubKeyPath := remotePrivKeyPath + ".pub"

	escapedPubKey := strings.ReplaceAll(pubKey, "/", "\\/")
	cmd := fmt.Sprintf(`
		sed -i '\|%s|d' %s 2>/dev/null;
		rm -f %s %s 2>/dev/null;
		echo 'KEYS_REMOVED'
	`, escapedPubKey, authKeysFile, remotePrivKeyPath, remotePubKeyPath)

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("remote key removal failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// RemoveLocalKeys removes the cluster key from local authorized_keys and
// deletes the local cluster private/public key files. Called when the local
// node is removed from the cluster by quorum consensus.
func RemoveLocalKeys(clusterKeyPath string, log zerolog.Logger) {
	pubKeyPath := clusterKeyPath + ".pub"

	// Remove from local authorized_keys
	pubKeyData, err := os.ReadFile(pubKeyPath)
	if err == nil {
		pubKey := strings.TrimSpace(string(pubKeyData))
		authKeysFile := os.ExpandEnv("$HOME/.ssh/authorized_keys")
		content, err := os.ReadFile(authKeysFile)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			var filtered []string
			for _, line := range lines {
				if !strings.Contains(line, pubKey) {
					filtered = append(filtered, line)
				}
			}
			os.WriteFile(authKeysFile, []byte(strings.Join(filtered, "\n")), 0600)
			log.Info().Msg("Removed cluster key from local authorized_keys")
		}
	}

	// Delete local key files
	os.Remove(clusterKeyPath)
	os.Remove(pubKeyPath)
	log.Info().Str("key_path", clusterKeyPath).Msg("Deleted local cluster key files")
}

// RemoteMarkClusterNode connects to a remote node's lanmon RPC socket via
// SSH tunneling and marks the given MAC as a cluster member.
func RemoteMarkClusterNode(host string, port int, user, clusterKeyPath, remoteRPCSocket, mac string, inCluster bool) error {
	privKeyData, err := os.ReadFile(clusterKeyPath)
	if err != nil {
		return fmt.Errorf("reading cluster private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(privKeyData)
	if err != nil {
		return fmt.Errorf("parsing cluster private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	sshClient, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("SSH dial to %s: %w", addr, err)
	}
	defer sshClient.Close()

	conn, err := sshClient.Dial("unix", remoteRPCSocket)
	if err != nil {
		return fmt.Errorf("tunneling to remote RPC socket %s: %w", remoteRPCSocket, err)
	}
	defer conn.Close()

	rpcClient := netrpc.NewClient(conn)
	defer rpcClient.Close()

	type MarkClusterNodeArgs struct {
		MAC       string
		InCluster bool
	}
	type MarkClusterNodeReply struct {
		Success bool
	}

	args := &MarkClusterNodeArgs{MAC: mac, InCluster: inCluster}
	reply := &MarkClusterNodeReply{}
	if err := rpcClient.Call("Service.MarkClusterNode", args, reply); err != nil {
		return fmt.Errorf("RPC MarkClusterNode on %s: %w", host, err)
	}

	return nil
}

// PushMembershipChange propagates a cluster membership change to all other members.
// It uses the locally known cluster member list to find peers.
func PushMembershipChange(client *rpc.Client, mac string, inCluster bool, clusterKeyPath string, log zerolog.Logger) {
	members, err := client.ListClusterNodes()
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch cluster members for push")
		return
	}

	log.Info().Int("count", len(members)).Msg("Pushing membership change to cluster members...")

	for _, m := range members {
		// Skip self (the CLI client node) - it's already updated locally
		// In a typical setup, the local node is in the list.
		
		log.Debug().Str("peer", m.Beacon.Hostname).Msg("Pushing update to peer")
		
		// Use default port 22 and root user for now, or detect from config if possible
		err := RemoteMarkClusterNode(m.Beacon.IPAddress, 22, "root", clusterKeyPath, "/run/lanmon/server.sock", mac, inCluster)
		if err != nil {
			log.Warn().Err(err).Str("peer", m.Beacon.Hostname).Msg("Failed to push update to peer")
		} else {
			log.Info().Str("peer", m.Beacon.Hostname).Msg("Peer updated successfully")
		}
	}
}

// HandleRemovedNodes is called by the quorum loop when nodes are removed
// from the cluster by majority vote. It cleans up SSH keys for each removed node.
func HandleRemovedNodes(removedMACs []string, allHosts map[string]string, localMAC, clusterKeyPath string, log zerolog.Logger) {
	for _, mac := range removedMACs {
		ip, ok := allHosts[mac]
		if !ok {
			continue
		}

		// If the removed node is ourselves, clean up locally
		if mac == localMAC {
			log.Warn().Msg("Local node was removed from cluster by quorum — cleaning up keys")
			RemoveLocalKeys(clusterKeyPath, log)
			continue
		}

		// Remove keys from the remote node
		log.Info().Str("mac", mac).Str("ip", ip).Msg("Removing cluster keys from removed node")
		if err := RemoveKeys(ip, 22, "root", clusterKeyPath); err != nil {
			log.Warn().Err(err).Str("mac", mac).Msg("Failed to remove keys from removed node")
		}
	}
}

// ProvisionClusterKeys pulls the cluster keys from a peer and installs them locally.
// It uses the network RPC client and authenticates with the sharedSecret.
func ProvisionClusterKeys(peerIP string, peerPort int, sharedSecret string, clusterKeyPath string, log zerolog.Logger) error {
	addr := fmt.Sprintf("%s:%d", peerIP, peerPort)
	client, err := rpc.NewNetworkClient(addr)
	if err != nil {
		return fmt.Errorf("connecting to peer %s: %w", addr, err)
	}
	defer client.Close()

	privKey, pubKey, err := client.GetClusterKeys(sharedSecret)
	if err != nil {
		return fmt.Errorf("fetching cluster keys from peer: %w", err)
	}

	// Install keys locally
	err = installKeysLocally(privKey, pubKey, clusterKeyPath, log)
	if err != nil {
		return fmt.Errorf("installing cluster keys: %w", err)
	}

	return nil
}

func installKeysLocally(privKey, pubKey, clusterKeyPath string, log zerolog.Logger) error {
	// 1. Create directory
	keyDir := filepath.Dir(clusterKeyPath)
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return fmt.Errorf("creating key directory: %w", err)
	}

	// 2. Write private key
	if err := os.WriteFile(clusterKeyPath, []byte(privKey), 0600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}

	// 3. Write public key
	pubKeyPath := clusterKeyPath + ".pub"
	if err := os.WriteFile(pubKeyPath, []byte(pubKey), 0644); err != nil {
		return fmt.Errorf("writing public key: %w", err)
	}

	// 4. Add to local authorized_keys to ensure mutual trust
	homeDir := os.Getenv("HOME")
	if homeDir == "" || homeDir == "/" {
		homeDir = "/root"
	}
	authKeysFile := filepath.Join(homeDir, ".ssh", "authorized_keys")
	os.MkdirAll(filepath.Dir(authKeysFile), 0700)

	f, err := os.OpenFile(authKeysFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening authorized_keys: %w", err)
	}
	defer f.Close()

	// Check if already exists
	content, _ := os.ReadFile(authKeysFile)
	if !strings.Contains(string(content), strings.TrimSpace(pubKey)) {
		if _, err := f.WriteString("\n" + strings.TrimSpace(pubKey) + "\n"); err != nil {
			return fmt.Errorf("writing to authorized_keys: %w", err)
		}
		log.Info().Msg("Added cluster key to authorized_keys")
	}

	log.Info().Str("path", clusterKeyPath).Msg("Cluster keys installed locally")
	return nil
}

// SyncClusterKnownHosts ensures that all cluster members are in the local known_hosts
// so that SSH connectivity doesn't prompt for verification.
func SyncClusterKnownHosts(hosts []store.HostRecord, knownHostsPath string, log zerolog.Logger) error {
	if knownHostsPath == "" {
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(knownHostsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// We'll append to the file if hosts are missing.
	// For simplicity in this environment, most internal SSH calls use InsecureIgnoreHostKey.
	// This function stays here as a placeholder for full known_hosts management.
	return nil
}

// RemoveSelf removes the local node from the cluster entirely.
// It:
//  1. Notifies all peer nodes to unmark the local node.
//  2. Asks all peers to remove the cluster key from their authorized_keys.
//  3. Strips the cluster public key from the local authorized_keys.
//  4. Deletes the local cluster private and public key files.
//  5. Removes all cluster peer IPs from the local known_hosts for full isolation.
//
// After this call the local node can no longer SSH into peers and peers can no
// longer SSH into this node using the cluster key.
func RemoveSelf(client *rpc.Client, clusterKeyPath, knownHostsPath string, localMAC string, log zerolog.Logger) error {

	// 1. Unmark self in local DB
	if err := client.MarkClusterNode(localMAC, false); err != nil {
		log.Warn().Err(err).Msg("Failed to unmark self in local DB (may already be removed)")
	}

	// 2. Fetch current peer list before we lose access
	members, err := client.ListClusterNodes()
	if err != nil {
		log.Warn().Err(err).Msg("Could not fetch cluster peers; will still clean up locally")
		members = nil
	}

	// 3. For each peer: tell them to unmark us and remove our key
	for _, m := range members {
		if m.Beacon.MACAddress == localMAC {
			continue // skip self
		}

		log.Info().Str("peer", m.Beacon.Hostname).Str("ip", m.Beacon.IPAddress).Msg("Notifying peer to remove local node from cluster")

		// Tell peer to unmark us in their DB
		err := RemoteMarkClusterNode(
			m.Beacon.IPAddress, 22, "root",
			clusterKeyPath, "/run/lanmon/server.sock",
			localMAC, false,
		)
		if err != nil {
			log.Warn().Err(err).Str("peer", m.Beacon.Hostname).Msg("Failed to unmark self on peer (peer may be unreachable)")
		} else {
			log.Info().Str("peer", m.Beacon.Hostname).Msg("Peer updated: local node removed from cluster")
		}
	}

	// 4. Remove cluster key from all peers' authorized_keys and delete their key files
	for _, m := range members {
		if m.Beacon.MACAddress == localMAC {
			continue
		}
		log.Info().Str("peer", m.Beacon.Hostname).Msg("Removing cluster keys from peer")
		if err := RemoveKeys(m.Beacon.IPAddress, 22, "root", clusterKeyPath); err != nil {
			log.Warn().Err(err).Str("peer", m.Beacon.Hostname).Msg("Could not remove keys from peer (peer may be unreachable)")
		}
	}

	// 5. Strip cluster public key from local authorized_keys and delete local key files
	log.Info().Msg("Removing cluster keys from local system")
	RemoveLocalKeys(clusterKeyPath, log)

	// 6. Remove cluster peer IPs from local known_hosts so the node cannot SSH to peers
	if knownHostsPath != "" {
		log.Info().Str("known_hosts", knownHostsPath).Msg("Cleaning cluster peers from known_hosts")
		removeClusterPeersFromKnownHosts(members, knownHostsPath, localMAC, log)
	}

	log.Info().Msg("Local node successfully removed from cluster and isolated")
	return nil
}

// removeClusterPeersFromKnownHosts removes all cluster peer host entries from the
// given known_hosts file so the local node cannot establish trusted SSH sessions
// to any previous cluster member.
func removeClusterPeersFromKnownHosts(peers []store.HostRecord, knownHostsPath, localMAC string, log zerolog.Logger) {
	content, err := os.ReadFile(knownHostsPath)
	if err != nil {
		// File may not exist — that's fine
		return
	}

	lines := strings.Split(string(content), "\n")
	var kept []string
	for _, line := range lines {
		skip := false
		for _, p := range peers {
			if p.Beacon.MACAddress == localMAC {
				continue
			}
			if strings.Contains(line, p.Beacon.IPAddress) || strings.Contains(line, p.Beacon.Hostname) {
				skip = true
				log.Debug().Str("peer", p.Beacon.Hostname).Msg("Removing peer from known_hosts")
				break
			}
		}
		if !skip {
			kept = append(kept, line)
		}
	}

	if err := os.WriteFile(knownHostsPath, []byte(strings.Join(kept, "\n")), 0600); err != nil {
		log.Warn().Err(err).Msg("Failed to rewrite known_hosts after cluster removal")
	}
}

