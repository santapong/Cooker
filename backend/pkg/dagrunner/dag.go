// Package dagrunner provides a reusable DAG (Directed Acyclic Graph) execution engine.
// It executes nodes in topological order with support for parallel execution of independent nodes.
package dagrunner

import "fmt"

// Node represents a single unit of work in the DAG.
type Node struct {
	ID   string
	Deps []string // IDs of nodes this node depends on
}

// DAG represents a directed acyclic graph of nodes.
type DAG struct {
	Nodes map[string]*Node
}

// NewDAG creates an empty DAG.
func NewDAG() *DAG {
	return &DAG{Nodes: make(map[string]*Node)}
}

// AddNode adds a node to the DAG.
func (d *DAG) AddNode(id string, deps []string) {
	d.Nodes[id] = &Node{ID: id, Deps: deps}
}

// TopologicalSort returns nodes in execution order. Returns an error if a cycle is detected.
func (d *DAG) TopologicalSort() ([][]string, error) {
	inDegree := make(map[string]int)
	adj := make(map[string][]string)

	for id := range d.Nodes {
		inDegree[id] = 0
	}
	for id, node := range d.Nodes {
		for _, dep := range node.Deps {
			if _, ok := d.Nodes[dep]; !ok {
				return nil, fmt.Errorf("node %s depends on unknown node %s", id, dep)
			}
			adj[dep] = append(adj[dep], id)
			inDegree[id]++
		}
	}

	// Group nodes into levels (nodes in the same level can run in parallel)
	var levels [][]string
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		levels = append(levels, queue)
		var nextQueue []string
		for _, id := range queue {
			visited++
			for _, next := range adj[id] {
				inDegree[next]--
				if inDegree[next] == 0 {
					nextQueue = append(nextQueue, next)
				}
			}
		}
		queue = nextQueue
	}

	if visited != len(d.Nodes) {
		return nil, fmt.Errorf("DAG contains a cycle")
	}

	return levels, nil
}

// Validate checks the DAG for cycles and missing dependencies.
func (d *DAG) Validate() []string {
	var errors []string

	for id, node := range d.Nodes {
		for _, dep := range node.Deps {
			if _, ok := d.Nodes[dep]; !ok {
				errors = append(errors, fmt.Sprintf("node %s references unknown dependency %s", id, dep))
			}
		}
	}

	_, err := d.TopologicalSort()
	if err != nil {
		errors = append(errors, err.Error())
	}

	return errors
}
