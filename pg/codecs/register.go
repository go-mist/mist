package codecs

// Register records a type in the default registry. The same name overwrites a built-in codec.
func Register(name string, codec Codec) {
	Default.Register(name, codec)
}

// RegisterOID points an OID at a type name in the default registry.
func RegisterOID(oid uint32, name string) {
	Default.RegisterOID(oid, name)
}
