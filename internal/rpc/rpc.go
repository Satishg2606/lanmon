// Package rpc provides Unix socket IPC between the lanmon server and connect CLI,
// and TCP RPC for inter-node cluster key distribution.
package rpc

import (
	"fmt"
	"net"
	netrpc "net/rpc"
	"os"
	"time"

	"github.com/rs/zerolog"

	"lanmon/internal/beacon"
	"lanmon/internal/store"
)

// Service is the RPC service exposed by the server.
type Service struct {
	store          *store.Store
	log            zerolog.Logger
	sharedSecret   string
	clusterKeyPath string
}

// GetClusterKeysArgs is the request for GetClusterKeys.
type GetClusterKeysArgs struct {
	Timestamp int64
	Signature []byte // HMAC of timestamp
}

// GetClusterKeysReply is the response for GetClusterKeys.
// PrivateKey and PublicKey are encrypted using AES-GCM with the shared secret.
type GetClusterKeysReply struct {
	EncryptedPrivateKey []byte
	EncryptedPublicKey  []byte
}

// ListActiveHostsArgs is the request for ListActiveHosts.
type ListActiveHostsArgs struct{}

// ListActiveHostsReply is the response for ListActiveHosts.
type ListActiveHostsReply struct {
	Hosts []store.HostRecord
}

// MarkKeyPushedArgs is the request for MarkKeyPushed.
type MarkKeyPushedArgs struct {
	MAC string
}

// MarkKeyPushedReply is the response for MarkKeyPushed.
type MarkKeyPushedReply struct {
	Success bool
}

// MarkClusterNodeArgs is the request for MarkClusterNode.
type MarkClusterNodeArgs struct {
	MAC       string
	InCluster bool
}

// MarkClusterNodeReply is the response for MarkClusterNode.
type MarkClusterNodeReply struct {
	Success bool
}

// ListClusterNodesArgs is the request for ListClusterNodes.
type ListClusterNodesArgs struct{}

// ListClusterNodesReply is the response for ListClusterNodes.
type ListClusterNodesReply struct {
	Hosts []store.HostRecord
}

// UpsertHostArgs is the request for UpsertHost.
type UpsertHostArgs struct {
	Payload beacon.BeaconPayload
}

// UpsertHostReply is the response for UpsertHost.
type UpsertHostReply struct {
	Success bool
}

// ListActiveHosts returns all active host records.
func (s *Service) ListActiveHosts(args *ListActiveHostsArgs, reply *ListActiveHostsReply) error {
	hosts, err := s.store.GetActive()
	if err != nil {
		return fmt.Errorf("fetching active hosts: %w", err)
	}
	reply.Hosts = hosts
	return nil
}

// MarkKeyPushed marks the SSH key as pushed for the given MAC address.
func (s *Service) MarkKeyPushed(args *MarkKeyPushedArgs, reply *MarkKeyPushedReply) error {
	if err := s.store.MarkKeyPushed(args.MAC); err != nil {
		return fmt.Errorf("marking key pushed: %w", err)
	}
	reply.Success = true
	return nil
}

// MarkClusterNode marks a host as a cluster member or not.
func (s *Service) MarkClusterNode(args *MarkClusterNodeArgs, reply *MarkClusterNodeReply) error {
	if err := s.store.MarkClusterNode(args.MAC, args.InCluster); err != nil {
		return fmt.Errorf("marking cluster node: %w", err)
	}
	reply.Success = true
	return nil
}

// ListClusterNodes returns all nodes marked as cluster members.
func (s *Service) ListClusterNodes(args *ListClusterNodesArgs, reply *ListClusterNodesReply) error {
	hosts, err := s.store.GetClusterNodes()
	if err != nil {
		return fmt.Errorf("fetching cluster nodes: %w", err)
	}
	reply.Hosts = hosts
	return nil
}

// UpsertHost inserts or updates a host record (used to add local node to DB).
func (s *Service) UpsertHost(args *UpsertHostArgs, reply *UpsertHostReply) error {
	if err := s.store.Upsert(args.Payload); err != nil {
		return fmt.Errorf("upserting host: %w", err)
	}
	reply.Success = true
	return nil
}

// GetClusterKeys returns the cluster private and public keys if the request is authenticated.
// The keys are encrypted using AES-GCM with the shared secret for secure transit.
func (s *Service) GetClusterKeys(args *GetClusterKeysArgs, reply *GetClusterKeysReply) error {
	// 1. Verify HMAC signature of the timestamp
	tsData := []byte(fmt.Sprintf("%d", args.Timestamp))
	if !beacon.VerifyHMAC(args.Signature, tsData, s.sharedSecret) {
		s.log.Warn().Msg("Unauthorized GetClusterKeys attempt (HMAC mismatch)")
		return fmt.Errorf("unauthorized")
	}

	// 2. Anti-replay: Timestamp must be within 60 seconds
	now := time.Now().Unix()
	if args.Timestamp < now-60 || args.Timestamp > now+60 {
		s.log.Warn().Int64("ts", args.Timestamp).Msg("Unauthorized GetClusterKeys attempt (stale timestamp)")
		return fmt.Errorf("unauthorized")
	}

	// 3. Read keys
	privKey, err := os.ReadFile(s.clusterKeyPath)
	if err != nil {
		return fmt.Errorf("reading cluster private key: %w", err)
	}

	pubKey, err := os.ReadFile(s.clusterKeyPath + ".pub")
	if err != nil {
		return fmt.Errorf("reading cluster public key: %w", err)
	}

	// 4. Encrypt keys with shared secret
	encPriv, err := beacon.Encrypt(privKey, s.sharedSecret)
	if err != nil {
		return fmt.Errorf("encrypting private key: %w", err)
	}

	encPub, err := beacon.Encrypt(pubKey, s.sharedSecret)
	if err != nil {
		return fmt.Errorf("encrypting public key: %w", err)
	}

	reply.EncryptedPrivateKey = encPriv
	reply.EncryptedPublicKey = encPub

	s.log.Info().Msg("Encrypted cluster keys shared with peer via RPC")
	return nil
}

// StartServer starts the Unix socket RPC server for local CLI.
func StartServer(socketPath string, db *store.Store, log zerolog.Logger, sharedSecret, clusterKeyPath string) error {
	service := &Service{
		store:          db,
		log:            log,
		sharedSecret:   sharedSecret,
		clusterKeyPath: clusterKeyPath,
	}

	server := netrpc.NewServer()
	if err := server.Register(service); err != nil {
		return fmt.Errorf("registering RPC service: %w", err)
	}

	// Remove existing socket file if present
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", socketPath, err)
	}

	// Set socket permissions
	if err := os.Chmod(socketPath, 0660); err != nil {
		log.Warn().Err(err).Msg("Failed to set socket permissions")
	}

	log.Info().Str("socket", socketPath).Msg("RPC server started")

	go server.Accept(listener)
	return nil
}

// StartNetworkServer starts a TCP RPC server for inter-node communication.
func StartNetworkServer(addr string, db *store.Store, log zerolog.Logger, sharedSecret, clusterKeyPath string) error {
	service := &Service{
		store:          db,
		log:            log,
		sharedSecret:   sharedSecret,
		clusterKeyPath: clusterKeyPath,
	}

	server := netrpc.NewServer()
	if err := server.Register(service); err != nil {
		return fmt.Errorf("registering network RPC service: %w", err)
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on network RPC %s: %w", addr, err)
	}

	log.Info().Str("addr", addr).Msg("Network RPC server started")

	go server.Accept(listener)
	return nil
}

// Client is a client for the lanmon RPC service.
type Client struct {
	client *netrpc.Client
}

// NewClient dials the Unix socket and returns an RPC client.
func NewClient(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connecting to RPC socket %s: %w", socketPath, err)
	}
	return &Client{client: netrpc.NewClient(conn)}, nil
}

// NewNetworkClient dials a TCP address and returns an RPC client.
func NewNetworkClient(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connecting to Network RPC %s: %w", addr, err)
	}
	return &Client{client: netrpc.NewClient(conn)}, nil
}

// Close closes the RPC client connection.
func (c *Client) Close() error {
	return c.client.Close()
}

// ListActiveHosts fetches all active hosts from the server.
func (c *Client) ListActiveHosts() ([]store.HostRecord, error) {
	args := &ListActiveHostsArgs{}
	reply := &ListActiveHostsReply{}
	if err := c.client.Call("Service.ListActiveHosts", args, reply); err != nil {
		return nil, err
	}
	return reply.Hosts, nil
}

// MarkKeyPushed tells the server to mark a host's SSH key as pushed.
func (c *Client) MarkKeyPushed(mac string) error {
	args := &MarkKeyPushedArgs{MAC: mac}
	reply := &MarkKeyPushedReply{}
	return c.client.Call("Service.MarkKeyPushed", args, reply)
}

// MarkClusterNode tells the server to mark a host as a cluster member.
func (c *Client) MarkClusterNode(mac string, inCluster bool) error {
	args := &MarkClusterNodeArgs{MAC: mac, InCluster: inCluster}
	reply := &MarkClusterNodeReply{}
	return c.client.Call("Service.MarkClusterNode", args, reply)
}

// ListClusterNodes fetches all cluster nodes from the server.
func (c *Client) ListClusterNodes() ([]store.HostRecord, error) {
	args := &ListClusterNodesArgs{}
	reply := &ListClusterNodesReply{}
	if err := c.client.Call("Service.ListClusterNodes", args, reply); err != nil {
		return nil, err
	}
	return reply.Hosts, nil
}

// UpsertHost inserts or updates a host record in the local database.
func (c *Client) UpsertHost(payload beacon.BeaconPayload) error {
	args := &UpsertHostArgs{Payload: payload}
	reply := &UpsertHostReply{}
	return c.client.Call("Service.UpsertHost", args, reply)
}

// GetClusterKeys pulls the cluster keys from a peer node.
// The keys are decrypted using AES-GCM with the shared secret after reception.
func (c *Client) GetClusterKeys(sharedSecret string) (string, string, error) {
	ts := time.Now().Unix()
	sig := beacon.ComputeHMAC([]byte(fmt.Sprintf("%d", ts)), sharedSecret)

	args := &GetClusterKeysArgs{
		Timestamp: ts,
		Signature: sig,
	}
	reply := &GetClusterKeysReply{}
	if err := c.client.Call("Service.GetClusterKeys", args, reply); err != nil {
		return "", "", err
	}

	// Decrypt keys
	privKey, err := beacon.Decrypt(reply.EncryptedPrivateKey, sharedSecret)
	if err != nil {
		return "", "", fmt.Errorf("decrypting private key: %w", err)
	}

	pubKey, err := beacon.Decrypt(reply.EncryptedPublicKey, sharedSecret)
	if err != nil {
		return "", "", fmt.Errorf("decrypting public key: %w", err)
	}

	return string(privKey), string(pubKey), nil
}
