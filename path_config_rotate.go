package artifactory

import (
	"context"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

func (b *backend) pathConfigRotate() *framework.Path {
	return &framework.Path{
		Pattern: "config/rotate",
		Fields: map[string]*framework.FieldSchema{
			"username": {
				Type:        framework.TypeString,
				Description: "Optional. Override Artifactory token username for new admin token.",
			},
			"description": {
				Type:        framework.TypeString,
				Description: "Optional. Set Artifactory token description on new admin token.",
			},
		},
		Operations: map[logical.Operation]framework.OperationHandler{
			logical.UpdateOperation: &framework.PathOperation{
				Callback: b.pathConfigRotateWrite,
				Summary:  "Rotate the Artifactory Admin Token.",
			},
		},
		HelpSynopsis: `Rotate the Artifactory Admin Token.`,
		HelpDescription: `
This will rotate the "access_token" used to access artifactory from this plugin, and revoke the old admin token.
`,
	}
}

func (b *backend) pathConfigRotateWrite(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	b.configMutex.Lock()
	defer b.configMutex.Unlock()

	config, err := b.fetchAdminConfiguration(ctx, req.Storage)
	if err != nil {
		return nil, err
	}

	if config == nil {
		return logical.ErrorResponse("backend not configured"), nil
	}

	oldAccessToken := config.AccessToken

	// Parse Current Token (to get tokenID/scope)
	token, err := b.getTokenInfo(*config, oldAccessToken)
	if err != nil {
		return logical.ErrorResponse("error parsing existing AccessToken: " + err.Error()), err
	}

	// Check for submitted username
	if val, ok := data.GetOk("username"); ok {
		b.Logger().Debug("old username: " + token.Username)
		token.Username = val.(string)
		b.Logger().Debug("new username: " + token.Username)
	}

	if len(token.Username) == 0 {
		token.Username = "admin-vault-secrets-artifactory" // default username if empty
	}
	b.Logger().Debug("oldToken ID: " + token.TokenID)

	// Create admin role for the new token
	role := &artifactoryRole{
		Username: token.Username,
		Scope:    token.Scope,
	}

	// Check for new description
	if val, ok := data.GetOk("description"); ok {
		role.Description = val.(string)
	}

	// Create a new token
	resp, err := b.CreateToken(*config, *role)
	if err != nil {
		return logical.ErrorResponse("error creating new token"), err
	}
	b.Logger().Debug("newTokenID: " + resp.TokenId)

	// Set new token
	config.AccessToken = resp.AccessToken

	// Save new config
	entry, err := logical.StorageEntryJSON("config/admin", config)
	if err != nil {
		return nil, err
	}

	err = req.Storage.Put(ctx, entry)
	if err != nil {
		return nil, err
	}

	// Invalidate Old Token
	oldSecret := logical.Secret{
		InternalData: map[string]interface{}{
			"access_token": oldAccessToken,
			"token_id":     token.TokenID,
		},
	}
	err = b.RevokeToken(*config, oldSecret)
	if err != nil {
		return logical.ErrorResponse("error revoking existing AccessToken"), err
	}

	return nil, nil
}
