// Package hosts manages the local /etc/hosts file for hostname resolution of discovered nodes.
package hosts

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"lanmon/internal/store"
)

const (
	hostsPath    = "/etc/hosts"
	beginMarker = "# BEGIN LANMON MANAGED HOSTS"
	endMarker   = "# END LANMON MANAGED HOSTS"
)

// Sync updates /etc/hosts with all active hosts from the database.
func Sync(db *store.Store) error {
	// Check if we have root permissions (usually needed for /etc/hosts)
	if os.Geteuid() != 0 {
		return fmt.Errorf("insufficient permissions to modify /etc/hosts (must be root)")
	}

	hosts, err := db.GetAll()
	if err != nil {
		return fmt.Errorf("getting hosts from db: %w", err)
	}

	file, err := os.Open(hostsPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", hostsPath, err)
	}
	defer file.Close()

	var newLines []string
	scanner := bufio.NewScanner(file)
	inManagedSection := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		
		if strings.HasPrefix(trimmed, beginMarker) {
			inManagedSection = true
			continue
		}
		if strings.HasPrefix(trimmed, endMarker) {
			inManagedSection = false
			continue
		}
		if !inManagedSection {
			newLines = append(newLines, line)
		}
	}

	// Build the new managed section
	var managedLines []string
	managedLines = append(managedLines, beginMarker)
	
	for _, h := range hosts {

		if h.Beacon.Hostname != "" && h.Beacon.IPAddress != "" {
			// Avoid duplicate entries if multiple IPs map to same name 
			// (though in this system it's 1:1)
			entry := fmt.Sprintf("%-16s %s", h.Beacon.IPAddress, h.Beacon.Hostname)
			managedLines = append(managedLines, entry)
		}
	}
	managedLines = append(managedLines, endMarker)

	// Append managed section to the end of preserved lines
	// Ensure there's a newline before our section if the file didn't end with one
	if len(newLines) > 0 && newLines[len(newLines)-1] != "" {
		newLines = append(newLines, "")
	}
	newLines = append(newLines, managedLines...)

	// Write back
	content := strings.Join(newLines, "\n") + "\n"
	err = os.WriteFile(hostsPath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", hostsPath, err)
	}

	return nil
}

