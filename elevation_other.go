//go:build !windows

package main

func ensureAdministrator(args []string) (bool, error) {
	return false, nil
}
