package agent

import "testing"

func TestServiceDefinitionsIncludeSingBoxAnd1Panel(t *testing.T) {
	wanted := map[string]bool{"sing-box": false, "1Panel": false}
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
