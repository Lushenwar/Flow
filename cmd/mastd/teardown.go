package main

import (
	"log"

	"github.com/Lushenwar/Flow/internal/enforce"
	"github.com/Lushenwar/Flow/internal/paths"
)

// clearEnforcement undoes every layer from a standalone process, without the
// daemon running.
//
// It works because teardown is driven by on-disk state rather than by what any
// process remembers: ResolverPin restores from its backup file, and the hosts
// layer rewrites its marked block. Both are readable by whoever runs uninstall.
func clearEnforcement() error {
	e := enforce.New(false, nil, enforce.Layers(paths.Dir(), false)...)
	e.Clear()
	log.Printf("cleared enforcement layers")
	return nil
}
