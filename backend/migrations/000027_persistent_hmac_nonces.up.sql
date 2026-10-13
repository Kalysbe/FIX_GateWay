CREATE TABLE hmac_nonces (
    nonce_hash BYTEA PRIMARY KEY CHECK(octet_length(nonce_hash)=32),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX hmac_nonces_expiry_idx ON hmac_nonces(expires_at);
