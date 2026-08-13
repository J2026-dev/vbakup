package agent

import "testing"

func TestServiceDefinitionsIncludeSupportedRecoveryServices(t *testing.T) {
	wanted := map[string]bool{"sing-box": false, "1Panel": false, "vless-all-in-one": false, "Docker": false}
	for _, definition := range serviceDefinitions() {
		if _, ok := wanted[definition.Name]; ok {
			wanted[definition.Name] = true
		}
	}
	for name, found := range wanted {
		if !found {
			t.Fatalf("service definition %q is missing", name)
		}
	}
	for _, definition := range serviceDefinitions() {
		if definition.Name == "vless-all-in-one" && !contains(definition.Paths, "/etc/vless-reality") {
			t.Fatal("vless-all-in-one persistent database path is missing")
		}
		if definition.Name == "Docker" && definition.Runtime != "docker" {
			t.Fatal("Docker runtime recovery marker is missing")
		}
	}
}

func TestParseSystemdPropertiesIsIndependentOfOutputOrder(t *testing.T) {
	properties := parseSystemdProperties("FragmentPath=/etc/systemd/system/sing-box.service\nUnitFileState=enabled\nLoadState=loaded\nActiveState=active\n")
	if properties["LoadState"] != "loaded" || properties["ActiveState"] != "active" || properties["UnitFileState"] != "enabled" || properties["FragmentPath"] != "/etc/systemd/system/sing-box.service" {
		t.Fatalf("properties=%v", properties)
	}
}

func TestShortcutManifestLabelsAndRestoreScope(t *testing.T) {
	discovery := Discovery{ShortcutCommands: []ShortcutCommand{{Name: "1pctl", Path: "/usr/local/bin/1pctl", Kind: "executable", Restorable: true}}}
	labels := ShortcutNames(discovery)
	if len(labels) != 1 || labels[0] != "1pctl (/usr/local/bin/1pctl)" {
		t.Fatalf("labels=%v", labels)
	}
	if !isRestorableShortcut("/usr/local/bin/custom-command") || isRestorableShortcut("/usr/bin/system-command") {
		t.Fatal("shortcut restore scope is incorrect")
	}
}
