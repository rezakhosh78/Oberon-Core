package main

import (
	"fmt"
	"os"
	"strings"
)

func applyWarpProfileToConfig(content string, profile warpProfile) (string, error) {
	sectionOrder := []string{"interface", "peer"}
	keyOrder := map[string][]string{
		"interface": {"privatekey", "address", "mtu"},
		"peer":      {"publickey", "endpoint"},
	}
	keyLabels := map[string]string{
		"privatekey": "PrivateKey",
		"address":    "Address",
		"mtu":        "MTU",
		"publickey":  "PublicKey",
		"endpoint":   "Endpoint",
	}
	settings := map[string]map[string]string{
		"interface": {
			"privatekey": profile.PrivateKey,
			"address":    profile.Address,
			"mtu":        fmt.Sprint(profile.MTU),
		},
		"peer": {
			"publickey": profile.PeerPublicKey,
			"endpoint":  profile.Endpoint,
		},
	}
	seen := map[string]map[string]bool{
		"interface": {},
		"peer":      {},
	}
	lines := strings.Split(content, "\n")
	section := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			continue
		}
		withoutComment := strings.SplitN(line, "#", 2)[0]
		keyPart, _, ok := strings.Cut(withoutComment, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(keyPart))
		if section == "peer" && key == "presharedkey" {
			lines[index] = ""
			continue
		}
		value, ok := settings[section][key]
		if !ok {
			continue
		}
		left, comment, hasComment := strings.Cut(line, "#")
		prefix, _, _ := strings.Cut(left, "=")
		lines[index] = strings.TrimRight(prefix, " \t") + " = " + value
		if hasComment {
			lines[index] += " " + strings.TrimSpace("#"+comment)
		}
		seen[section][key] = true
	}

	for _, targetSection := range sectionOrder {
		sectionLine := -1
		for index, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.EqualFold(trimmed, "["+targetSection+"]") {
				sectionLine = index
				break
			}
		}
		if sectionLine < 0 {
			return "", fmt.Errorf("profile has no [%s] section", targetSection)
		}
		var missing []string
		for _, key := range keyOrder[targetSection] {
			value := settings[targetSection][key]
			if !seen[targetSection][key] {
				missing = append(missing, keyLabels[key]+" = "+value)
			}
		}
		if len(missing) == 0 {
			continue
		}
		insertAt := sectionLine + 1
		updated := make([]string, 0, len(lines)+len(missing))
		updated = append(updated, lines[:insertAt]...)
		updated = append(updated, missing...)
		updated = append(updated, lines[insertAt:]...)
		lines = updated
	}
	return strings.Join(lines, "\n"), nil
}

func backupProfile(path string, content []byte) (string, error) {
	for index := 0; index < 100; index++ {
		backupPath := path + ".bak"
		if index > 0 {
			backupPath = fmt.Sprintf("%s.bak.%d", path, index)
		}
		file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := file.Write(content); err != nil {
			_ = file.Close()
			_ = os.Remove(backupPath)
			return "", err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			_ = os.Remove(backupPath)
			return "", err
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(backupPath)
			return "", err
		}
		return backupPath, nil
	}
	return "", fmt.Errorf("could not reserve a profile backup filename")
}
