# HCP Terraform Private Registry Setup Guide

This guide walks you through publishing the Railway Terraform provider to your Acendas HCP Terraform Cloud private registry.

## Prerequisites

- HCP Terraform Cloud account with the "acendas" organization
- GitHub repository access (this repo)
- GPG key pair for signing releases

## Step 1: Generate GPG Key

If you don't have a GPG key, create one:

```bash
# Generate a new GPG key
gpg --full-generate-key

# Choose:
# - RSA and RSA (default)
# - 4096 bits
# - Key does not expire (0)
# - Enter your name and email
# - Create a strong passphrase
```

Get your key fingerprint:

```bash
gpg --list-secret-keys --keyid-format LONG

# Output example:
# sec   rsa4096/ABCDEF1234567890 2024-01-01 [SC]
#       FINGERPRINT1234567890ABCDEF1234567890ABCDEF
# The fingerprint is the 40-character string
```

Export your private key (for GitHub Actions):

```bash
gpg --armor --export-secret-keys YOUR_KEY_ID > private.key
```

Export your public key (for HCP Terraform):

```bash
gpg --armor --export YOUR_KEY_ID > public.key
```

## Step 2: Add GitHub Secrets

Go to your repository **Settings > Secrets and variables > Actions** and add:

| Secret Name | Value |
|-------------|-------|
| `GPG_PRIVATE_KEY` | Contents of `private.key` (ASCII-armored private key) |
| `PASSPHRASE` | Your GPG key passphrase |

## Step 3: Configure HCP Terraform Cloud

### 3.1 Create Provider in HCP Terraform

1. Log in to [HCP Terraform](https://app.terraform.io)
2. Go to **Registry > Providers > Publish**
3. Click **Publish a private provider**
4. Connect your GitHub account if not already connected
5. Select the repository: `terraform-provider-railway`
6. Provider name should be: `railway`
7. Namespace: `acendas`

### 3.2 Upload GPG Public Key

1. Go to **Settings > Providers > GPG Keys**
2. Click **Add GPG Key**
3. Paste the contents of your `public.key` file
4. This key will be used to verify signed releases

### 3.3 Verify Webhook (Auto-created)

HCP Terraform will automatically create a webhook on your GitHub repository. When you push a new version tag (e.g., `v0.7.0`), it will:

1. Trigger the GitHub Actions release workflow
2. Build and sign the provider binaries
3. Create a GitHub release
4. HCP Terraform webhook detects the release
5. Provider version is automatically published to your private registry

## Step 4: Create a Release

To publish a new version:

```bash
# Ensure you're on the main branch with all changes committed
git checkout master
git pull

# Create a version tag
git tag v0.7.0

# Push the tag to trigger the release
git push origin v0.7.0
```

The release workflow will:
- Build binaries for all platforms (linux, darwin, windows)
- Sign the checksums with your GPG key
- Create a GitHub release with all artifacts
- HCP Terraform will automatically import the release

## Step 5: Use the Private Provider

In your Terraform configurations, use the private provider:

```hcl
terraform {
  required_providers {
    railway = {
      source  = "app.terraform.io/acendas/railway"
      version = "~> 0.7.0"
    }
  }
}

provider "railway" {
  token = var.railway_token
}
```

### HCP Terraform Workspace Configuration

If using HCP Terraform workspaces, ensure the workspace has:

1. **Railway API Token**: Add as a sensitive environment variable:
   - Key: `RAILWAY_TOKEN`
   - Value: Your Railway API token

## Troubleshooting

### Release Not Appearing in Registry

1. Check GitHub Actions for release workflow errors
2. Verify GPG key is correctly imported in the workflow
3. Check HCP Terraform webhook delivery logs

### GPG Signing Failures

1. Ensure `GPG_PRIVATE_KEY` secret contains the full ASCII-armored key
2. Verify `PASSPHRASE` is correct
3. Check the key hasn't expired

### Provider Not Found

1. Ensure you're authenticated to HCP Terraform: `terraform login`
2. Verify the source address matches exactly: `app.terraform.io/acendas/railway`
3. Check your organization has access to the provider

## Version History

| Version | Features |
|---------|----------|
| v0.7.0 | Service instance config (serverless, health checks, restart policies, build config), Service limits resource, Data sources, Private networking |

## Support

For issues with:
- **This provider**: Open an issue in this repository
- **HCP Terraform**: Contact HashiCorp support
- **Railway API**: Check [Railway documentation](https://docs.railway.app)
