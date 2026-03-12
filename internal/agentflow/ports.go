package agentflow

import (
	"fmt"
	"net"
)

var preferredPortAllocator = allocatePreferredPort

func allocatePreferredPort(start, end int, reserved map[int]struct{}) (int, error) {
	for port := start; port <= end; port++ {
		if reserved != nil {
			if _, exists := reserved[port]; exists {
				continue
			}
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		_ = listener.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no available port in range %d-%d", start, end)
}
