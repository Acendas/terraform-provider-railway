#!/bin/bash
set -e

# Publish Terraform provider to HCP Terraform Private Registry
# This script is called after GoReleaser creates a GitHub release

# Required environment variables:
# - TF_CLOUD_TOKEN: HCP Terraform API token
# - TF_ORG: HCP Terraform organization name
# - GPG_KEY_ID: GPG key ID used to sign the release
# - VERSION: Provider version (without 'v' prefix)
# - GITHUB_REPOSITORY: GitHub repository (e.g., "acendas/terraform-provider-railway")

PROVIDER_NAME="railway"
API_BASE="https://app.terraform.io/api/v2"
REGISTRY_API="https://app.terraform.io/api/registry/private/v2"

# Validate required environment variables
for var in TF_CLOUD_TOKEN TF_ORG GPG_KEY_ID VERSION; do
    if [ -z "${!var}" ]; then
        echo "Error: $var environment variable is required"
        exit 1
    fi
done

echo "Publishing terraform-provider-${PROVIDER_NAME} v${VERSION} to HCP Terraform..."

# Create working directory for downloads
WORK_DIR=$(mktemp -d)
trap "rm -rf $WORK_DIR" EXIT

# Download release assets from GitHub
echo "Downloading release assets..."
cd "$WORK_DIR"

ASSET_PREFIX="terraform-provider-${PROVIDER_NAME}_${VERSION}"
gh release download "v${VERSION}" \
    --pattern "${ASSET_PREFIX}_SHA256SUMS" \
    --pattern "${ASSET_PREFIX}_SHA256SUMS.sig" \
    --pattern "${ASSET_PREFIX}_*.zip"

# Verify files were downloaded
if [ ! -f "${ASSET_PREFIX}_SHA256SUMS" ]; then
    echo "Error: SHA256SUMS file not found"
    exit 1
fi

# Create provider version
echo "Creating provider version ${VERSION}..."
VERSION_RESPONSE=$(curl -s -X POST \
    "${API_BASE}/organizations/${TF_ORG}/registry-providers/private/${TF_ORG}/${PROVIDER_NAME}/versions" \
    -H "Authorization: Bearer ${TF_CLOUD_TOKEN}" \
    -H "Content-Type: application/vnd.api+json" \
    -d "{
        \"data\": {
            \"type\": \"registry-provider-versions\",
            \"attributes\": {
                \"version\": \"${VERSION}\",
                \"key-id\": \"${GPG_KEY_ID}\",
                \"protocols\": [\"5.0\"]
            }
        }
    }")

# Check for errors
if echo "$VERSION_RESPONSE" | jq -e '.errors' > /dev/null 2>&1; then
    echo "Error creating version:"
    echo "$VERSION_RESPONSE" | jq '.errors'
    exit 1
fi

# Extract upload URLs
SHASUMS_UPLOAD_URL=$(echo "$VERSION_RESPONSE" | jq -r '.data.links."shasums-upload"')
SHASUMS_SIG_UPLOAD_URL=$(echo "$VERSION_RESPONSE" | jq -r '.data.links."shasums-sig-upload"')

if [ "$SHASUMS_UPLOAD_URL" = "null" ] || [ "$SHASUMS_SIG_UPLOAD_URL" = "null" ]; then
    echo "Error: Failed to get upload URLs from response"
    echo "$VERSION_RESPONSE" | jq '.'
    exit 1
fi

# Upload SHA256SUMS
echo "Uploading SHA256SUMS..."
curl -s -X PUT "$SHASUMS_UPLOAD_URL" \
    -H "Content-Type: application/octet-stream" \
    --data-binary "@${ASSET_PREFIX}_SHA256SUMS"

# Upload SHA256SUMS.sig
echo "Uploading SHA256SUMS.sig..."
curl -s -X PUT "$SHASUMS_SIG_UPLOAD_URL" \
    -H "Content-Type: application/octet-stream" \
    --data-binary "@${ASSET_PREFIX}_SHA256SUMS.sig"

# Create platforms and upload binaries
echo "Creating platforms and uploading binaries..."

# Parse SHA256SUMS to get all platforms
while IFS= read -r line; do
    SHASUM=$(echo "$line" | awk '{print $1}')
    FILENAME=$(echo "$line" | awk '{print $2}')

    # Skip non-zip files (like manifest.json)
    if [[ ! "$FILENAME" =~ \.zip$ ]]; then
        continue
    fi

    # Extract OS and arch from filename
    # Format: terraform-provider-railway_0.8.0_linux_amd64.zip
    OS=$(echo "$FILENAME" | sed -E "s/terraform-provider-${PROVIDER_NAME}_${VERSION}_([^_]+)_([^.]+)\.zip/\1/")
    ARCH=$(echo "$FILENAME" | sed -E "s/terraform-provider-${PROVIDER_NAME}_${VERSION}_([^_]+)_([^.]+)\.zip/\2/")

    echo "  Creating platform ${OS}/${ARCH}..."

    PLATFORM_RESPONSE=$(curl -s -X POST \
        "${API_BASE}/organizations/${TF_ORG}/registry-providers/private/${TF_ORG}/${PROVIDER_NAME}/versions/${VERSION}/platforms" \
        -H "Authorization: Bearer ${TF_CLOUD_TOKEN}" \
        -H "Content-Type: application/vnd.api+json" \
        -d "{
            \"data\": {
                \"type\": \"registry-provider-version-platforms\",
                \"attributes\": {
                    \"os\": \"${OS}\",
                    \"arch\": \"${ARCH}\",
                    \"shasum\": \"${SHASUM}\",
                    \"filename\": \"${FILENAME}\"
                }
            }
        }")

    # Check for errors
    if echo "$PLATFORM_RESPONSE" | jq -e '.errors' > /dev/null 2>&1; then
        echo "  Error creating platform ${OS}/${ARCH}:"
        echo "$PLATFORM_RESPONSE" | jq '.errors'
        continue
    fi

    BINARY_UPLOAD_URL=$(echo "$PLATFORM_RESPONSE" | jq -r '.data.links."provider-binary-upload"')

    if [ "$BINARY_UPLOAD_URL" = "null" ]; then
        echo "  Error: Failed to get binary upload URL for ${OS}/${ARCH}"
        continue
    fi

    echo "  Uploading ${FILENAME}..."
    curl -s -X PUT "$BINARY_UPLOAD_URL" \
        -H "Content-Type: application/octet-stream" \
        --data-binary "@${FILENAME}"

done < "${ASSET_PREFIX}_SHA256SUMS"

echo ""
echo "Successfully published terraform-provider-${PROVIDER_NAME} v${VERSION} to HCP Terraform!"
echo "View at: https://app.terraform.io/app/${TF_ORG}/registry/providers/private/${TF_ORG}/${PROVIDER_NAME}/${VERSION}"
