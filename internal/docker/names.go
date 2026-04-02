package docker

// ContainerName returns the Docker container name for an application.
// All containers are prefixed with "qd-" for easy identification.
func ContainerName(appName string) string {
	return "qd-" + appName
}

// ServiceName returns the Docker service name for an application.
// Service names match the app name (without prefix).
func ServiceName(appName string) string {
	return appName
}
