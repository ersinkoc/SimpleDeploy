package docker

import "testing"

func TestContainerName(t *testing.T) {
	tests := []struct {
		appName  string
		expected string
	}{
		{"myapp", "qd-myapp"},
		{"app-123", "qd-app-123"},
		{"test_app", "qd-test_app"},
		{"a", "qd-a"},
	}

	for _, tt := range tests {
		result := ContainerName(tt.appName)
		if result != tt.expected {
			t.Errorf("ContainerName(%q) = %q, want %q", tt.appName, result, tt.expected)
		}
	}
}

func TestServiceName(t *testing.T) {
	tests := []struct {
		appName  string
		expected string
	}{
		{"myapp", "myapp"},
		{"app-123", "app-123"},
	}

	for _, tt := range tests {
		result := ServiceName(tt.appName)
		if result != tt.expected {
			t.Errorf("ServiceName(%q) = %q, want %q", tt.appName, result, tt.expected)
		}
	}
}
