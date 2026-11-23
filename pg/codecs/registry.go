package codecs

import "sync"

// Collection looks up a codec by type name and OID. It returns nil when none is found, and does not treat an unknown type as a string.
type Collection struct {
	mu     sync.RWMutex
	byName map[string]Codec
	byOID  map[uint32]string
}

// NewCollection returns an empty registry.
func NewCollection() *Collection {
	return &Collection{
		byName: map[string]Codec{},
		byOID:  map[uint32]string{},
	}
}

// Register records a codec by type name. The same name overwrites. A nil codec deletes that name.
func (c *Collection) Register(name string, codec Codec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if codec == nil {
		delete(c.byName, name)
		return
	}
	c.byName[name] = codec
}

// RegisterOID points an OID at a type name already registered.
func (c *Collection) RegisterOID(oid uint32, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byOID[oid] = name
}

// Get looks up by type name. It returns nil when nothing is registered.
func (c *Collection) Get(name string) Codec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.byName[name]
}

// GetOID looks up by OID. It returns nil when nothing is registered.
func (c *Collection) GetOID(oid uint32) Codec {
	c.mu.RLock()
	name := c.byOID[oid]
	codec := c.byName[name]
	c.mu.RUnlock()
	return codec
}

// Bind binds a registered type to a column. When nothing is registered it returns an error and does not silently pass through.
func (c *Collection) Bind(name, column string, dimensions int) (Bound, error) {
	codec := c.Get(name)
	if codec == nil {
		return Bound{}, errf("pg: no codec registered for type %s", name)
	}
	sqlType := name
	if dimensions > 0 {
		sqlType = Array{BaseType: name, Dimensions: dimensions}.sqlType()
	}
	return Bind(column, sqlType, dimensions, codec), nil
}

// Default is the in-process default registry. Built-in types are registered in init.
var Default = NewCollection()

func init() {
	registerBuiltins(Default)
}

func registerBuiltins(c *Collection) {
	c.Register("numeric", Numeric)
	c.Register("decimal", Numeric)
	c.Register("json", JSON)
	c.Register("jsonb", JSONB)
	c.Register("bytea", Bytea)
	c.Register("interval", Interval)
	c.Register("uuid", UUID)
	c.Register("date", Date)
	c.Register("time", Time)
	c.Register("timestamp", Timestamp)
	c.Register("timestamptz", Timestamptz)
	c.Register("smallint", Int2)
	c.Register("integer", Int4)
	c.Register("bigint", Int8)
	textArray := Array{BaseType: "text", Dimensions: 1}
	int4Array := Array{BaseType: "int4", Dimensions: 1}
	c.Register("text[]", textArray)
	c.Register("int4[]", int4Array)
	// https://www.postgresql.org/docs/current/datatype-oid.html
	c.RegisterOID(1700, "numeric")
	c.RegisterOID(114, "json")
	c.RegisterOID(3802, "jsonb")
	c.RegisterOID(17, "bytea")
	c.RegisterOID(1186, "interval")
	c.RegisterOID(2950, "uuid")
	c.RegisterOID(1082, "date")
	c.RegisterOID(1083, "time")
	c.RegisterOID(1114, "timestamp")
	c.RegisterOID(1184, "timestamptz")
	c.RegisterOID(21, "smallint")
	c.RegisterOID(23, "integer")
	c.RegisterOID(20, "bigint")
	c.RegisterOID(1009, "text[]")
	c.RegisterOID(1007, "int4[]")
}
