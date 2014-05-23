package config

import (
	"fmt"
)

// Load loads the Terraform configuration from a given file.
//
// This file can be any format that Terraform recognizes, and import any
// other format that Terraform recognizes.
func Load(path string) (*Config, error) {
	importTree, err := loadTree(path)
	if err != nil {
		return nil, err
	}

	configTree, err := importTree.ConfigTree()
	if err != nil {
		return nil, err
	}

	return configTree.Flatten()
}

// configurable is an interface that must be implemented by any configuration
// formats of Terraform in order to return a *Config.
type configurable interface {
	Config() (*Config, error)
}

// importTree is the result of the first-pass load of the configuration
// files. It is a tree of raw configurables and then any children (their
// imports).
//
// An importTree can be turned into a configTree.
type importTree struct {
	Path     string
	Raw      configurable
	Children []*importTree
}

// This is the function type that must be implemented by the configuration
// file loader to turn a single file into a configurable and any additional
// imports.
type fileLoaderFunc func(path string) (configurable, []string, error)

// loadTree takes a single file and loads the entire importTree for that
// file. This function detects what kind of configuration file it is an
// executes the proper fileLoaderFunc.
func loadTree(root string) (*importTree, error) {
	c, imps, err := loadFileLibucl(root)
	if err != nil {
		return nil, err
	}

	children := make([]*importTree, len(imps))
	for i, imp := range imps {
		t, err := loadTree(imp)
		if err != nil {
			return nil, err
		}

		children[i] = t
	}

	return &importTree{
		Path:     root,
		Raw:      c,
		Children: children,
	}, nil
}

// ConfigTree traverses the importTree and turns each node into a *Config
// object, ultimately returning a *configTree.
func (t *importTree) ConfigTree() (*configTree, error) {
	config, err := t.Raw.Config()
	if err != nil {
		return nil, fmt.Errorf(
			"Error loading %s: %s",
			t.Path,
			err)
	}

	// Build our result
	result := &configTree{
		Path:   t.Path,
		Config: config,
	}

	// TODO: Follow children and load them

	return result, nil
}
