package model

import "time"

// ContainerInfo represents a Docker container.
type ContainerInfo struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	Status  string            `json:"status"`
	State   string            `json:"state"` // running, exited, paused
	Ports   []PortBinding     `json:"ports"`
	Created time.Time         `json:"created"`
	Labels  map[string]string `json:"labels"`
}

// PortBinding maps container ports to host ports.
type PortBinding struct {
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort"`
	Protocol      string `json:"protocol"` // tcp, udp
}

// ImageInfo represents a Docker/OCI image.
type ImageInfo struct {
	ID          string   `json:"id"`
	RepoTags    []string `json:"repoTags"`
	RepoDigests []string `json:"repoDigests"`
	Size        int64    `json:"size"`
	Created     time.Time `json:"created"`
	Layers      int      `json:"layers"`
}
