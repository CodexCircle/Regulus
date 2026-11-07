package storage

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Database types.
const (
	TypeSQLite   = "sqlite"
	TypePostgres = "postgres"
)

// SSL modes accepted for PostgreSQL, as the server's libpq names them.
var sslModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// Config says which database the hub uses and how to reach it.
type Config struct {
	// Type is TypeSQLite (the default) or TypePostgres.
	Type string
	// DataDir holds the SQLite file; with PostgreSQL it still holds the keys.
	DataDir string

	// The rest applies to PostgreSQL only.
	Host        string
	Port        int
	Name        string
	User        string
	Password    string
	SSLMode     string
	SSLRootCert string
	// Schema is where the tables live (search_path). Empty means "public".
	Schema string
}

// IsPostgres reports whether the configuration selects PostgreSQL.
func (c Config) IsPostgres() bool { return c.Type == TypePostgres }

// Validate reports whether the configuration is usable.
func (c Config) Validate() error {
	switch c.Type {
	case "", TypeSQLite:
		return nil
	case TypePostgres:
	default:
		return fmt.Errorf("database.type %q is invalid (want %s or %s)", c.Type, TypeSQLite, TypePostgres)
	}
	if c.Host == "" {
		return errors.New("database.host must be set when database.type is postgres")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("database.port must be a number from 1 to 65535")
	}
	if c.Name == "" {
		return errors.New("database.name must not be empty")
	}
	if c.User == "" {
		return errors.New("database.user must not be empty")
	}
	if !contains(sslModes, c.SSLMode) {
		return fmt.Errorf("database.sslmode %q is invalid (want %s)", c.SSLMode, strings.Join(sslModes, ", "))
	}
	if !c.ValidSchema() {
		return fmt.Errorf("database.schema %q is invalid (letters, digits and underscore, not starting with a digit)", c.Schema)
	}
	return nil
}

// ValidSchema reports whether Schema is empty or a plain identifier.
func (c Config) ValidSchema() bool { return ValidSchema(c.Schema) }

// ValidSchema reports whether name is empty or a plain identifier.
func ValidSchema(name string) bool { return name == "" || identifierRE(name) }

func identifierRE(s string) bool {
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return s != ""
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Describe names the backend for logs and status output. It never includes
// the password.
func (c Config) Describe() string {
	if !c.IsPostgres() {
		return "SQLite (" + FileName + " in " + c.DataDir + ")"
	}
	return fmt.Sprintf("PostgreSQL (%s@%s/%s)", c.User, net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), c.Name)
}

// URL builds the connection URL of a PostgreSQL configuration. It contains the
// password: use it only to connect, never to print.
func (c Config) URL() string {
	q := url.Values{}
	q.Set("sslmode", c.SSLMode)
	if c.SSLRootCert != "" {
		q.Set("sslrootcert", c.SSLRootCert)
	}
	q.Set("connect_timeout", "10")
	q.Set("application_name", "regulus")
	if c.Schema != "" {
		// public stays on the path: the citext extension lives there.
		q.Set("search_path", c.Schema+",public")
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     "/" + c.Name,
		RawQuery: q.Encode(),
	}
	return u.String()
}
