// Copyright (c) 2024 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package state_test

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
)

type identitiesSuite struct{}

var _ = Suite(&identitiesSuite{})

// Generated using `openssl req -new -x509 -out cert.pem -days 3650 -subj "/CN=canonical.com"`
const validPEMX509Cert = `-----BEGIN CERTIFICATE-----
MIIBRDCB96ADAgECAhROTkdEcgeil5/5NUNTq1ZRPDLiPTAFBgMrZXAwGDEWMBQG
A1UEAwwNY2Fub25pY2FsLmNvbTAeFw0yNTA5MDgxNTI2NTJaFw0zNTA5MDYxNTI2
NTJaMBgxFjAUBgNVBAMMDWNhbm9uaWNhbC5jb20wKjAFBgMrZXADIQDtxRqb9EMe
ffcoJ0jNn9ys8uDFeHnQ6JRxgNFvomDTHqNTMFEwHQYDVR0OBBYEFI/oHjhG1A7F
3HM7McXP7w7CxtrwMB8GA1UdIwQYMBaAFI/oHjhG1A7F3HM7McXP7w7CxtrwMA8G
A1UdEwEB/wQFMAMBAf8wBQYDK2VwA0EA40v4eckaV7RBXyRb0sfcCcgCAGYtiCSD
jwXVTUH4HLpbhK0RAaEPOL4h5jm36CrWTkxzpbdCrIu4NgPLQKJ6Cw==
-----END CERTIFICATE-----
`

const invalidPEMX509Cert = `-----BEGIN CERTIFICATE-----
MIIBEjCBxQIUbhv2Dwr9CY4ApHMo2ilg6FC/8RMwBQYDK2VwMCwxFDASBgNVBAMM
C2V4YW1wbGUuY29tMRQwEgYDVQQKDAtFeGFtcGxlIE9yZzAeFw0yNTA5MjYxNTI3
MDJaFw0yNjA5MjYxNTI3MDJaMCwxFDASBgNVBAMMC2V4YW1wbGUuY29tMRQwEgYD
VQQKDAtFeGFtcGxlIE9yZzAqMAUGAytlcAMhAIlut+P3huKtFK439Ap+7U4Bv4r2
DY3fLYnfNEcrXTdLMAUGAytlcANBAEhUiFSTNuCuu2rc4pqGwXYGEtEFqRZDZwYe
mHLySscsVEgGwncFhL/9UW5iZl/tO/o+WiyVd/K4Vk0Yrp6uggA=
-----END CERTIFICATE-----
`

// Generated using `openssl req -new -newkey ed25519 -out bad-cert.pem -nodes -subj "/CN=canonical.com"`
// This is a valid PEM block but not a valid X.509 certificate.
const testPEMPKCS10Req = `-----BEGIN CERTIFICATE REQUEST-----
MIGXMEsCAQAwGDEWMBQGA1UEAwwNY2Fub25pY2FsLmNvbTAqMAUGAytlcAMhADuu
TTkzIDS55kZukGFfsWM+kPug1hpJLVx4wKqr5eLNoAAwBQYDK2VwA0EA3QU93q5S
pV4RrgnD3G7kw2dg8fdJAZ/qn1bXToUzPy89uPMiAZIE+eHXBxzqTJ6GJrVY+2r7
GV6pXv511MycDg==
-----END CERTIFICATE REQUEST-----
`

// IMPORTANT NOTE: be sure secrets aren't included when adding to this!
func (s *identitiesSuite) TestMarshalAPI(c *C) {
	if !certAuthSupported {
		c.Skip("certificate authentication not supported in FIPS builds")
	}

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	err := st.AddIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
		"olivia": {
			Access: state.ReadAccess,
			Cert:   &state.CertIdentity{X509: parseCert(c, validPEMX509Cert)},
		},
	})
	c.Assert(err, IsNil)

	identities := st.Identities()
	data, err := json.MarshalIndent(identities, "", "    ")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `
{
    "bob": {
        "access": "read",
        "local": {
            "user-id": 42
        }
    },
    "mary": {
        "access": "admin",
        "local": {
            "user-id": 1000
        }
    },
    "nancy": {
        "access": "metrics",
        "basic": {
            "password": "*****"
        }
    },
    "olivia": {
        "access": "read",
        "cert": {
            "pem": "*****"
        }
    }
}`[1:])
}

func (s *identitiesSuite) TestUnmarshalAPI(c *C) {
	if !certAuthSupported {
		c.Skip("certificate authentication not supported in FIPS builds")
	}

	jsonCert, err := json.Marshal(validPEMX509Cert)
	c.Assert(err, IsNil)
	data := fmt.Appendf(nil, `
{
    "bob": {
        "access": "read",
        "local": {
            "user-id": 42
        }
    },
    "mary": {
        "access": "admin",
        "local": {
            "user-id": 1000
        }
    },
    "nancy": {
        "access": "metrics",
        "basic": {
            "password": "hash"
        }
    },
    "olivia": {
        "access": "read",
        "cert": {
            "pem": %s
        }
    }
}`, jsonCert)
	var identities map[string]*state.Identity
	err = json.Unmarshal(data, &identities)
	c.Assert(err, IsNil)
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
		"olivia": {
			Access: state.ReadAccess,
			Cert:   &state.CertIdentity{X509: parseCert(c, validPEMX509Cert)},
		},
	})
}

func (s *identitiesSuite) TestUnmarshalAPIErrors(c *C) {
	// Marshal a certificate request to test valid PEM but invalid X.509.
	jsonCertReq, err := json.Marshal(testPEMPKCS10Req)
	c.Assert(err, IsNil)
	// Marshal a certificate with extra data after the PEM block.
	jsonCertExtra, err := json.Marshal(validPEMX509Cert + "42")
	c.Assert(err, IsNil)

	tests := []struct {
		data  string
		error string
	}{{
		data:  `{"no-type": {"access": "admin"}}`,
		error: noTypeErrorMsg,
	}, {
		data:  `{"invalid-access": {"access": "admin", "local": {}}}`,
		error: `local identity must specify user-id`,
	}, {
		data:  `{"invalid-access": {"access": "metrics", "basic": {}}}`,
		error: `basic identity must specify password \(hashed\)`,
	}, {
		data:  `{"invalid-access": {"access": "read", "cert": {}}}`,
		error: certPEMRequiredError,
	}, {
		data:  `{"invalid-access": {"access": "read", "cert": {"pem": "..."}}}`,
		error: certPEMRequiredError,
	}, {
		data:  fmt.Sprintf(`{"invalid-access": {"access": "read", "cert": {"pem": %s}}}`, jsonCertReq),
		error: certParseError,
	}, {
		data:  fmt.Sprintf(`{"invalid-access": {"access": "read", "cert": {"pem": %s}}}`, jsonCertExtra),
		error: certExtraDataError,
	}, {
		data:  `{"invalid-access": {"access": "foo", "local": {"user-id": 42}}}`,
		error: `invalid access value "foo", must be "admin", "read", "metrics", or "untrusted"`,
	}, {
		data:  `{"invalid-access": {"local": {"user-id": 42}}}`,
		error: `access value must be specified \("admin", "read", "metrics", or "untrusted"\)`,
	}}
	for _, test := range tests {
		c.Logf("Input data: %s", test.data)
		var identities map[string]*state.Identity
		err := json.Unmarshal([]byte(test.data), &identities)
		c.Check(err, ErrorMatches, test.error)
	}
}

func (s *identitiesSuite) TestMarshalState(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	err := st.AddIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, IsNil)

	// Marshal entire state, then pull out just the "identities" key to test that.
	data, err := json.Marshal(st)
	c.Assert(err, IsNil)
	var unmarshalled map[string]any
	err = json.Unmarshal(data, &unmarshalled)
	c.Assert(err, IsNil)
	data, err = json.MarshalIndent(unmarshalled["identities"], "", "    ")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `
{
    "bob": {
        "access": "read",
        "local": {
            "user-id": 42
        }
    },
    "mary": {
        "access": "admin",
        "local": {
            "user-id": 1000
        }
    }
}`[1:])
}

func (s *identitiesSuite) TestUnmarshalState(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	data := []byte(`
{
    "identities": {
        "bob": {
            "access": "read",
            "local": {
                "user-id": 42
            }
        },
        "mary": {
            "access": "admin",
            "local": {
                "user-id": 1000
            }
        }
    }
}`)
	err := json.Unmarshal(data, &st)
	c.Assert(err, IsNil)
	c.Assert(st.Identities(), DeepEquals, map[string]*state.Identity{
		"bob": {
			Name:   "bob",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Name:   "mary",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
}

func (s *identitiesSuite) TestAddIdentities(c *C) {
	if !certAuthSupported {
		c.Skip("certificate authentication not supported in FIPS builds")
	}

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
		"olivia": {
			Access: state.ReadAccess,
			Cert:   &state.CertIdentity{X509: parseCert(c, validPEMX509Cert)},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	// Ensure they were added correctly (and Name fields have been set).
	identities := st.Identities()
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"bob": {
			Name:   "bob",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Name:   "mary",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Name:   "nancy",
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
		"olivia": {
			Name:   "olivia",
			Access: state.ReadAccess,
			Cert:   &state.CertIdentity{X509: parseCert(c, validPEMX509Cert)},
		},
	})

	// Can't add identity names that already exist.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, ErrorMatches, "identities already exist: bob, mary")

	// Can't add a nil identity.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": nil,
	})
	c.Assert(err, ErrorMatches, `identity "bill" invalid: identity must not be nil`)

	// Access value must be valid.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: "bar",
			Local:  &state.LocalIdentity{UserID: 43},
		},
	})
	c.Assert(err, ErrorMatches, `identity "bill" invalid: invalid access value "bar", must be "admin", "read", "metrics", or "untrusted"`)

	// Must have at least one type.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: "admin",
		},
	})
	c.Assert(err, ErrorMatches, `identity "bill" invalid: identity must have at least one type \("local", "basic", or "cert"\)`)

	// May have two types.
	err = st.AddIdentities(map[string]*state.Identity{
		"peter": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
			Local:  &state.LocalIdentity{UserID: 1001},
		},
	})
	c.Assert(err, IsNil)

	// Ensure user IDs are unique with existing users.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, ErrorMatches, `cannot have multiple identities with user ID 1000 \(bill, mary\)`)

	// Ensure user IDs are unique among the ones being added (and test >2 with same UID).
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 2000},
		},
		"bale": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 2000},
		},
		"boll": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 2000},
		},
	})
	c.Assert(err, ErrorMatches, `cannot have multiple identities with user ID 2000 \(bale, bill, boll\)`)
}

func (s *identitiesSuite) TestUpdateIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	err = st.UpdateIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"mary": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "new hash"},
		},
	})
	c.Assert(err, IsNil)

	// Ensure they were updated correctly.
	identities := st.Identities()
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"bob": {
			Name:   "bob",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"mary": {
			Name:   "mary",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"nancy": {
			Name:   "nancy",
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "new hash"},
		},
	})

	// Can't update identity names that don't exist.
	err = st.UpdateIdentities(map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"bale": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, ErrorMatches, "identities do not exist: bale, bill")

	// Ensure validation is being done (full testing done in AddIdentity).
	err = st.UpdateIdentities(map[string]*state.Identity{
		"bob": nil,
	})
	c.Assert(err, ErrorMatches, `identity "bob" invalid: identity must not be nil`)

	// Ensure unique user ID testing is being done (full testing done in AddIdentity).
	err = st.UpdateIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
	})
	c.Assert(err, ErrorMatches, `cannot have multiple identities with user ID 42 \(bob, mary\)`)
}

func (s *identitiesSuite) TestReplaceIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	err = st.ReplaceIdentities(map[string]*state.Identity{
		"bob": nil, // nil means remove it
		"mary": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"newguy": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 44},
		},
	})
	c.Assert(err, IsNil)

	// Ensure they were added/updated/deleted correctly.
	identities := st.Identities()
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"mary": {
			Name:   "mary",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"newguy": {
			Name:   "newguy",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 44},
		},
	})

	// Ensure validation is being done (full testing done in AddIdentity).
	err = st.ReplaceIdentities(map[string]*state.Identity{
		"bill": {
			Access: "admin",
		},
	})
	c.Assert(err, ErrorMatches, `identity "bill" invalid: `+noTypeErrorMsg)

	// Ensure unique user ID testing is being done (full testing done in AddIdentity).
	err = st.ReplaceIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
	})
	c.Assert(err, ErrorMatches, `cannot have multiple identities with user ID 43 \(bob, mary\)`)
}

func (s *identitiesSuite) TestRemoveIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
		"queen": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1001},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	err = st.RemoveIdentities(map[string]struct{}{
		"bob":   {},
		"mary":  {},
		"nancy": {},
	})
	c.Assert(err, IsNil)

	// Ensure they were removed correctly.
	identities := st.Identities()
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"bill": {
			Name:   "bill",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"queen": {
			Name:   "queen",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1001},
		},
	})

	// Can't remove identity names that don't exist.
	err = st.RemoveIdentities(map[string]struct{}{
		"bill": {},
		"bale": {},
		"mary": {},
	})
	c.Assert(err, ErrorMatches, "identities do not exist: bale, mary")
}

func (s *identitiesSuite) TestIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	// Ensure it returns correct results.
	identities := st.Identities()
	expected := map[string]*state.Identity{
		"bob": {
			Name:   "bob",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Name:   "mary",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Name:   "nancy",
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	}
	c.Assert(identities, DeepEquals, expected)

	// Ensure the map was cloned (mutations to first map won't affect second).
	identities2 := st.Identities()
	c.Assert(identities2, DeepEquals, expected)
	identities["changed"] = &state.Identity{}
	c.Assert(identities2, DeepEquals, expected)
}

func (s *identitiesSuite) TestIdentityFromInputs(c *C) {
	if !certAuthSupported {
		c.Skip("certificate authentication not supported in FIPS builds")
	}

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	ids := map[string]*state.Identity{
		"uid": {
			Access: state.MetricsAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"basic": {
			Access: state.ReadAccess,
			Basic: &state.BasicIdentity{
				// password: test
				Password: "$6$F9cFSVEKyO4gB1Wh$8S1BSKsNkF.jBAixGc4W7l80OpfCNk65LZBDHBng3NAmbcHuMj4RIm7992rrJ8YA.SJ0hvm.vGk2z483am4Ym1",
			},
		},
		"cert": {
			Access: state.AdminAccess,
			Cert:   &state.CertIdentity{X509: parseCert(c, validPEMX509Cert)},
		},
	}
	err := st.AddIdentities(ids)
	c.Assert(err, IsNil)

	validCert := parseCert(c, validPEMX509Cert)
	invalidCert := parseCert(c, invalidPEMX509Cert)

	tests := []struct {
		name           string
		userID         *uint32
		basicUser      string
		basicPass      string
		cert           *x509.Certificate
		expectedUser   string
		expectedAccess state.IdentityAccess
	}{{
		name:         "no inputs",
		expectedUser: "",
	}, {
		// Certificate authentication tests (highest priority)
		name:           "valid cert",
		cert:           validCert,
		expectedUser:   "cert",
		expectedAccess: state.AdminAccess,
	}, {
		name:         "invalid cert",
		cert:         invalidCert,
		expectedUser: "",
	}, {
		// Cert overrides other auth methods
		name:           "cert with basic auth ignored",
		cert:           validCert,
		basicUser:      "basic",
		basicPass:      "test",
		expectedUser:   "cert",
		expectedAccess: state.AdminAccess,
	}, {
		name:           "cert with uid ignored",
		cert:           validCert,
		userID:         ptr(uint32(42)),
		expectedUser:   "cert",
		expectedAccess: state.AdminAccess,
	}, {
		name:           "cert with both basic and uid ignored",
		cert:           validCert,
		basicUser:      "basic",
		basicPass:      "test",
		userID:         ptr(uint32(42)),
		expectedUser:   "cert",
		expectedAccess: state.AdminAccess,
	}, {
		// Basic authentication tests (medium priority)
		name:           "valid basic auth",
		basicUser:      "basic",
		basicPass:      "test",
		expectedUser:   "basic",
		expectedAccess: state.ReadAccess,
	}, {
		name:         "valid user invalid password",
		basicUser:    "basic",
		basicPass:    "wrong",
		expectedUser: "",
	}, {
		name:         "invalid user valid password",
		basicUser:    "nonexistent",
		basicPass:    "test",
		expectedUser: "",
	}, {
		name:         "invalid user invalid password",
		basicUser:    "nonexistent",
		basicPass:    "wrong",
		expectedUser: "",
	}, {
		name:         "empty user with password",
		basicUser:    "",
		basicPass:    "test",
		expectedUser: "",
	}, {
		name:         "user with empty password",
		basicUser:    "basic",
		basicPass:    "",
		expectedUser: "",
	}, {
		name:         "empty user and password",
		basicUser:    "",
		basicPass:    "",
		expectedUser: "",
	}, {
		// Basic auth overrides UID
		name:           "basic auth with uid ignored",
		basicUser:      "basic",
		basicPass:      "test",
		userID:         ptr(uint32(42)),
		expectedUser:   "basic",
		expectedAccess: state.ReadAccess,
	}, {
		name:         "invalid basic auth with valid uid ignored",
		basicUser:    "basic",
		basicPass:    "wrong",
		userID:       ptr(uint32(42)),
		expectedUser: "",
	}, {
		// Local/UID authentication tests (lowest priority)
		name:           "valid uid",
		userID:         ptr(uint32(42)),
		expectedUser:   "uid",
		expectedAccess: state.MetricsAccess,
	}, {
		name:         "invalid uid",
		userID:       ptr(uint32(100)),
		expectedUser: "",
	}, {
		// Edge cases
		name:         "nil uid",
		userID:       nil,
		expectedUser: "",
	}}

	for _, test := range tests {
		c.Logf("Running test: %s", test.name)
		identity := st.IdentityFromInputs(test.userID, test.basicUser, test.basicPass, test.cert)

		if test.expectedUser != "" {
			c.Assert(identity, NotNil)
			c.Assert(identity.Name, Equals, test.expectedUser)
			c.Assert(identity.Access, Equals, test.expectedAccess)
		} else {
			c.Assert(identity, IsNil)
		}
	}
}

func ptr[T any](v T) *T {
	return &v
}

func parseCert(c *C, pemBlock string) *x509.Certificate {
	block, _ := pem.Decode([]byte(pemBlock))
	c.Assert(block, NotNil)
	cert, _ := x509.ParseCertificate(block.Bytes)
	c.Assert(cert, NotNil)
	return cert
}
