/*
 * Copyright © 2015-2018 Aeneas Rekkas <aeneas+oss@aeneas.io>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * @author		Aeneas Rekkas <aeneas+oss@aeneas.io>
 * @copyright 	2015-2018 Aeneas Rekkas <aeneas+oss@aeneas.io>
 * @license 	Apache-2.0
 */

package oauth2_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/sessions"
	"github.com/julienschmidt/httprouter"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/herodot"
	hc "github.com/ory/hydra/client"
	"github.com/ory/hydra/jwk"
	. "github.com/ory/hydra/oauth2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2/clientcredentials"
	"gopkg.in/square/go-jose.v2"
)

func TestClientCredentials(t *testing.T) {
	for _, tc := range []struct {
		d                 string
		s                 oauth2.CoreStrategy
		assertAccessToken func(*testing.T, string)
	}{
		{
			d: "opaque",
			s: oauth2OpqaueStrategy,
		},
		{
			d: "jwt",
			s: oauth2JWTStrategy,
			assertAccessToken: func(t *testing.T, token string) {
				body, err := jwt.DecodeSegment(strings.Split(token, ".")[1])
				require.NoError(t, err)

				data := map[string]interface{}{}
				require.NoError(t, json.Unmarshal(body, &data))

				assert.EqualValues(t, "app-client", data["client_id"])
				assert.EqualValues(t, "app-client", data["sub"])
				assert.NotEmpty(t, data["iss"])
				assert.NotEmpty(t, data["jti"])
				assert.NotEmpty(t, data["exp"])
				assert.NotEmpty(t, data["iat"])
				assert.NotEmpty(t, data["nbf"])
				assert.EqualValues(t, data["nbf"], data["iat"])
				assert.EqualValues(t, []interface{}{"foobar"}, data["scp"])
			},
		},
	} {
		t.Run("tc="+tc.d, func(t *testing.T) {
			router := httprouter.New()
			l := logrus.New()
			l.Level = logrus.DebugLevel
			store := NewFositeMemoryStore(hc.NewMemoryManager(hasher), time.Second)

			jm := &jwk.MemoryManager{Keys: map[string]*jose.JSONWebKeySet{}}
			keys, err := (&jwk.RS256Generator{}).Generate("", "sig")
			require.NoError(t, err)
			require.NoError(t, jm.AddKeySet(OpenIDConnectKeyName, keys))
			jwtStrategy, err := jwk.NewRS256JWTStrategy(jm, OpenIDConnectKeyName)

			ts := httptest.NewServer(router)
			handler := &Handler{
				OAuth2: compose.Compose(
					fc,
					store,
					tc.s,
					nil,
					compose.OAuth2ClientCredentialsGrantFactory,
					compose.OAuth2TokenIntrospectionFactory,
				),
				//Consent:         consentStrategy,
				CookieStore:   sessions.NewCookieStore([]byte("foo-secret")),
				ForcedHTTP:    true,
				ScopeStrategy: fosite.HierarchicScopeStrategy,
				//IDTokenLifespan:   time.Minute,
				H:                 herodot.NewJSONWriter(l),
				L:                 l,
				IssuerURL:         ts.URL,
				OpenIDJWTStrategy: jwtStrategy,
			}

			handler.SetRoutes(router, router, func(h http.Handler) http.Handler {
				return h
			})

			require.NoError(t, store.CreateClient(&hc.Client{
				ClientID:      "app-client",
				Secret:        "secret",
				RedirectURIs:  []string{ts.URL + "/callback"},
				ResponseTypes: []string{"token"},
				GrantTypes:    []string{"client_credentials"},
				Scope:         "foobar",
			}))

			oauthClientConfig := &clientcredentials.Config{
				ClientID:     "app-client",
				ClientSecret: "secret",
				TokenURL:     ts.URL + "/oauth2/token",
				Scopes:       []string{"foobar"},
			}

			tok, err := oauthClientConfig.Token(context.Background())
			require.NoError(t, err)
			assert.NotEmpty(t, tok.AccessToken)
			if tc.assertAccessToken != nil {
				tc.assertAccessToken(t, tok.AccessToken)
			}
		})
	}
}
