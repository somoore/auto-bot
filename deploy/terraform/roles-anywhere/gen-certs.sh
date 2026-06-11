#!/usr/bin/env bash
# Generate a CA + leaf certificate for auto-bot's IAM Roles Anywhere setup.
#
#   ca.crt   -> pass to Terraform as ca_certificate_pem (the trust anchor)
#   leaf.crt -> store in the Kubernetes Secret the chart reads (awsRolesAnywhere.certSecret)
#   leaf.key -> store alongside leaf.crt (keep secret)
#
# Usage:  ./gen-certs.sh [common-name]   (default CN: auto-bot-pod)
set -euo pipefail

CN="${1:-auto-bot-pod}"
OUT="${OUT_DIR:-./certs}"
mkdir -p "$OUT"
cd "$OUT"

# --- CA (must have basicConstraints CA:TRUE, or the trust anchor is rejected) ---
openssl genrsa -out ca.key 4096
cat > ca-ext.cnf <<EOF
[req]
distinguished_name = dn
x509_extensions = v3_ca
prompt = no
[dn]
CN = auto-bot-rolesanywhere-ca
[v3_ca]
basicConstraints = critical,CA:TRUE
keyUsage = critical,keyCertSign,cRLSign
subjectKeyIdentifier = hash
EOF
openssl req -new -x509 -days 1825 -key ca.key -out ca.crt -config ca-ext.cnf

# --- Leaf cert (clientAuth EKU), signed by the CA ---
openssl genrsa -out leaf.key 2048
openssl req -new -key leaf.key -out leaf.csr -subj "/CN=${CN}"
cat > leaf-ext.cnf <<EOF
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature
extendedKeyUsage = clientAuth
EOF
openssl x509 -req -in leaf.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out leaf.crt -days 365 -sha256 -extfile leaf-ext.cnf
rm -f leaf.csr ca-ext.cnf leaf-ext.cnf

echo
echo "Done. Files in $OUT/:"
echo "  ca.crt   -> Terraform var ca_certificate_pem (e.g. ca_certificate_pem = file(\"certs/ca.crt\"))"
echo "  leaf.crt -> kubectl create secret generic auto-bot-ra-cert --from-file=leaf.crt --from-file=leaf.key"
echo "  leaf.key -> keep secret; never commit"
echo
echo "Leaf CN is '${CN}' — it must match the Terraform var certificate_cn."
