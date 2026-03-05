#!/bin/bash

# Hack script to fix ManagedClusterAddOn CRD storage version for integration tests
# This switches storage version from v1alpha1 to v1beta1

set -e

CRD_FILE="vendor/open-cluster-management.io/api/addon/v1beta1/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml"
BACKUP_DIR="${TEST_TMP:-/tmp}"
BACKUP_FILE="${BACKUP_DIR}/managedclusteraddons.crd.yaml.backup"

echo "Fixing CRD storage version in ${CRD_FILE}..."

# Check if file exists
if [ ! -f "${CRD_FILE}" ]; then
    echo "Error: CRD file not found at ${CRD_FILE}"
    exit 1
fi

# If backup exists, restore from backup first to ensure idempotency
if [ -f "${BACKUP_FILE}" ]; then
    echo "Restoring from backup to ensure clean state..."
    cp "${BACKUP_FILE}" "${CRD_FILE}"
else
    # Create backup if it doesn't exist
    cp "${CRD_FILE}" "${BACKUP_FILE}"
    echo "Created backup at ${BACKUP_FILE}"
fi

# Show original storage settings
echo ""
echo "Original storage settings:"
grep -n "storage:" "${CRD_FILE}" | while read -r line; do
    echo "  $line"
done

# Check if already in the desired state (v1alpha1: false, v1beta1: true)
first_storage=$(grep -m1 "storage:" "${CRD_FILE}" | grep -o "storage: [a-z]*")
second_storage=$(grep "storage:" "${CRD_FILE}" | tail -1 | grep -o "storage: [a-z]*")

if [ "$first_storage" = "storage: false" ] && [ "$second_storage" = "storage: true" ]; then
    echo ""
    echo "✓ Already in desired state (v1alpha1: false, v1beta1: true)"
    echo "  No changes needed"
    exit 0
fi

# Use a temporary marker to swap values
# Step 1: Change "storage: true" to a temporary marker
sed -i 's/storage: true/storage: TEMP_TRUE_MARKER/g' "${CRD_FILE}"

# Step 2: Change "storage: false" to "storage: true"
sed -i 's/storage: false/storage: true/g' "${CRD_FILE}"

# Step 3: Change the marker back to "storage: false"
sed -i 's/storage: TEMP_TRUE_MARKER/storage: false/g' "${CRD_FILE}"

# Verify the changes
echo ""
echo "✓ Storage settings updated:"
grep -n "storage:" "${CRD_FILE}" | while read -r line; do
    echo "  $line"
done
echo ""
echo "Backup saved at ${BACKUP_FILE}"
