-- positions:write no longer grants anything: the position write endpoints are removed.
UPDATE api_tokens
SET permissions = permissions - 'positions:write'
WHERE permissions ? 'positions:write';
