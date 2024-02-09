package pkgmgr

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	environmentsFilename = "environments.json"
)

type State struct {
	config       Config
	Environments []Environment
	// TODO: installed packages
}

func NewState(cfg Config) *State {
	return &State{
		config:       cfg,
		Environments: make([]Environment, 0),
	}
}

func (s *State) Load() error {
	if err := s.loadEnvironments(); err != nil {
		return err
	}
	return nil
}

func (s *State) Save() error {
	if err := s.saveEnvironments(); err != nil {
		return err
	}
	return nil
}

func (s *State) loadFile(filename string, dest any) error {
	tmpPath := filepath.Join(
		s.config.ConfigDir,
		filename,
	)
	// Check if the file exists and we can access it
	if _, err := os.Stat(tmpPath); err != nil {
		// Treat no file like an empty file
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, dest); err != nil {
		return err
	}
	return nil
}

func (s *State) saveFile(filename string, src any) error {
	// Create parent directory if it doesn't exist
	if _, err := os.Stat(s.config.ConfigDir); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(s.config.ConfigDir, os.ModePerm); err != nil {
				return err
			}
		}
	}
	tmpPath := filepath.Join(
		s.config.ConfigDir,
		filename,
	)
	jsonContent, err := json.Marshal(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmpPath, jsonContent, os.ModePerm); err != nil {
		return err
	}
	return nil
}

func (s *State) loadEnvironments() error {
	return s.loadFile(environmentsFilename, &(s.Environments))
}

func (s *State) saveEnvironments() error {
	return s.saveFile(environmentsFilename, &(s.Environments))
}
