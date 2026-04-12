package shun

// shunnableCodes lists error codes that represent persistent failures unlikely
// to self-resolve. These map to MySQL server error numbers and network-level
// failures that indicate misconfiguration rather than transient issues.
var shunnableCodes = map[string]bool{
	"mysql_error_1045": true, // Access denied (wrong password)
	"mysql_error_1044": true, // Access denied for database (permission)
	"mysql_error_1049": true, // Unknown database (config error)
	"mysql_error_1129": true, // Host blocked (max_connect_errors reached)

	"dns_resolution_failed": true, // Hostname doesn't resolve
	"connection_refused":    true, // Nothing listening on that port
	"ssl_error":             true, // TLS misconfiguration
	"tls_error":             true, // TLS misconfiguration (alternate code)
}

// IsShunnableMySQL returns true if the error code represents a persistent
// MySQL failure that won't self-resolve without human intervention.
func IsShunnableMySQL(errorCode string) bool {
	return shunnableCodes[errorCode]
}
