package identities

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// marshalledIdentity is used specifically for marshalling to the state
// database file. Unlike apiIdentity, it should include secrets.
type marshalledIdentity struct {
	Access string                   `json:"access"`
	Local  *marshalledLocalIdentity `json:"local,omitempty"`
	Basic  *marshalledBasicIdentity `json:"basic,omitempty"`
	Cert   *marshalledCertIdentity  `json:"cert,omitempty"`
}

type marshalledLocalIdentity struct {
	UserID uint32 `json:"user-id"`
}

type marshalledBasicIdentity struct {
	Password string `json:"password"`
}

type marshalledCertIdentity struct {
	PEM string `json:"pem"`
}

func marshalledIdentities(identities map[string]*Identity) map[string]*marshalledIdentity {
	marshalled := make(map[string]*marshalledIdentity, len(identities))
	for name, identity := range identities {
		marshalled[name] = &marshalledIdentity{
			Access: string(identity.Access),
		}
		if identity.Local != nil {
			marshalled[name].Local = &marshalledLocalIdentity{UserID: identity.Local.UserID}
		}
		if identity.Basic != nil {
			marshalled[name].Basic = &marshalledBasicIdentity{Password: identity.Basic.Password}
		}
		if identity.Cert != nil {
			pemBlock := &pem.Block{
				Type:  "CERTIFICATE",
				Bytes: identity.Cert.X509.Raw,
			}
			marshalled[name].Cert = &marshalledCertIdentity{PEM: string(pem.EncodeToMemory(pemBlock))}
		}
	}
	return marshalled
}

func unmarshalIdentities(marshalled map[string]*marshalledIdentity) (map[string]*Identity, error) {
	identities := make(map[string]*Identity, len(marshalled))
	for name, mi := range marshalled {
		identities[name] = &Identity{
			Name:   name,
			Access: IdentityAccess(mi.Access),
		}
		if mi.Local != nil {
			identities[name].Local = &LocalIdentity{UserID: mi.Local.UserID}
		}
		if mi.Basic != nil {
			identities[name].Basic = &BasicIdentity{Password: mi.Basic.Password}
		}
		if mi.Cert != nil {
			block, _ := pem.Decode([]byte(mi.Cert.PEM))
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("cannot parse certificate from cert identity: %w", err)
			}
			identities[name].Cert = &CertIdentity{X509: cert}
		}
	}
	return identities, nil
}
