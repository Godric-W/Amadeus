package sqlite

import (
	"net/url"
	"path/filepath"
)

func databaseDSN(path string) string {
	location := &url.URL{Scheme: "file", Path: sqliteURIPath(filepath.ToSlash(path))}
	query := location.Query()
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	location.RawQuery = query.Encode()
	return location.String()
}

func sqliteURIPath(path string) string {
	if isWindowsDrivePath(path) {
		return "/" + path
	}
	return path
}

func isWindowsDrivePath(path string) bool {
	return len(path) >= 3 && isASCIILetter(path[0]) && path[1] == ':' && path[2] == '/'
}

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
