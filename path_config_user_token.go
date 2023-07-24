package artifactory

import (
	"context"
	"time"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

func (b *backend) pathConfigUserToken() *framework.Path {
	return &framework.Path{
		Pattern: "config/user_token",
		Fields: map[string]*framework.FieldSchema{
			"audience": {
				Type:        framework.TypeString,
				Description: `Optional. See the JFrog Artifactory REST documentation on "Create Token" for a full and up to date description.`,
			},
			"default_ttl": {
				Type:        framework.TypeDurationSecond,
				Description: `Optional. Default TTL for issued user access tokens. If unset, uses the backend's default_ttl. Cannot exceed max_ttl.`,
			},
			"max_ttl": {
				Type:        framework.TypeDurationSecond,
				Description: `Optional. Maximum TTL that a user access token can be renewed for. If unset, uses the backend's max_ttl. Cannot exceed backend's max_ttl.`,
			},
			"default_description": {
				Type:        framework.TypeString,
				Description: `Optional. Default token description to set in Artifactory for issued user access tokens.`,
			},
		},
		Operations: map[logical.Operation]framework.OperationHandler{
			logical.UpdateOperation: &framework.PathOperation{
				Callback: b.pathConfigUserTokenUpdate,
				Summary:  "Configure the Artifactory secrets backend.",
			},
			logical.ReadOperation: &framework.PathOperation{
				Callback: b.pathConfigUserTokenRead,
				Summary:  "Examine the Artifactory secrets configuration.",
			},
		},
		HelpSynopsis:    `Configuration for issuing user tokens.`,
		HelpDescription: `Configures default values for the user_token/<user name> path.`,
	}
}

type userTokenConfiguration struct {
	Audience           string        `json:"audience,omitempty"`
	DefaultTTL         time.Duration `json:"default_ttl,omitempty"`
	MaxTTL             time.Duration `json:"max_ttl,omitempty"`
	DefaultDescription string        `json:"default_description,omitempty"`
}

// fetchAdminConfiguration will return nil,nil if there's no configuration
func (b *backend) fetchUserTokenConfiguration(ctx context.Context, storage logical.Storage) (*userTokenConfiguration, error) {
	var config userTokenConfiguration

	// Read in the backend configuration
	entry, err := storage.Get(ctx, "config/user_token")
	if err != nil {
		return nil, err
	}

	if entry == nil {
		return &userTokenConfiguration{}, nil
	}

	if err := entry.DecodeJSON(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func (b *backend) pathConfigUserTokenUpdate(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	b.configMutex.Lock()
	defer b.configMutex.Unlock()

	config, err := b.fetchAdminConfiguration(ctx, req.Storage)
	if err != nil {
		return nil, err
	}

	if config == nil {
		config = &adminConfiguration{}
	}

	go b.sendUsage(*config, "pathConfigUserTokenUpdate")

	userTokenConfig, err := b.fetchUserTokenConfiguration(ctx, req.Storage)
	if err != nil {
		return nil, err
	}

	if val, ok := data.GetOk("audience"); ok {
		userTokenConfig.Audience = val.(string)
	}

	if val, ok := data.GetOk("default_ttl"); ok {
		userTokenConfig.DefaultTTL = time.Duration(val.(int)) * time.Second
	}

	if val, ok := data.GetOk("max_ttl"); ok {
		userTokenConfig.MaxTTL = time.Duration(val.(int)) * time.Second
	}

	if val, ok := data.GetOk("default_description"); ok {
		userTokenConfig.DefaultDescription = val.(string)
	}

	entry, err := logical.StorageEntryJSON("config/user_token", userTokenConfig)
	if err != nil {
		return nil, err
	}

	err = req.Storage.Put(ctx, entry)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func (b *backend) pathConfigUserTokenRead(ctx context.Context, req *logical.Request, _ *framework.FieldData) (*logical.Response, error) {
	b.configMutex.RLock()
	defer b.configMutex.RUnlock()

	config, err := b.fetchAdminConfiguration(ctx, req.Storage)
	if err != nil {
		return nil, err
	}

	if config == nil {
		return logical.ErrorResponse("backend not configured"), nil
	}

	go b.sendUsage(*config, "pathConfigUserTokenRead")

	userTokenConfig, err := b.fetchUserTokenConfiguration(ctx, req.Storage)
	if err != nil {
		return nil, err
	}

	configMap := map[string]interface{}{
		"audience":            userTokenConfig.Audience,
		"default_ttl":         userTokenConfig.DefaultTTL.Seconds(),
		"max_ttl":             userTokenConfig.MaxTTL.Seconds(),
		"default_description": userTokenConfig.DefaultDescription,
	}

	// Optionally include token info if it parses properly
	token, err := b.getTokenInfo(*config, config.AccessToken)
	if err != nil {
		b.Logger().Warn("Error parsing AccessToken: " + err.Error())
	} else {
		configMap["token_id"] = token.TokenID
		configMap["username"] = token.Username
		configMap["scope"] = token.Scope
		if token.Expires > 0 {
			configMap["exp"] = token.Expires
			tm := time.Unix(token.Expires, 0)
			configMap["expires"] = tm.Local()
		}
	}

	return &logical.Response{
		Data: configMap,
	}, nil
}
