#!/usr/bin/env bash
set -euo pipefail

echo "This stores UDL secrets in macOS Keychain."
echo

read -r -s -p "Deezer ARL (required for deemix): " deemix_arl
echo
if [[ -z "${deemix_arl}" ]]; then
  echo "Skipped Deezer ARL (empty input)."
else
  security add-generic-password -U -s udl.deemix -a default -w "${deemix_arl}" >/dev/null
  echo "Saved Deezer ARL to keychain service=udl.deemix account=default"
fi

echo
read -r -p "Spotify Client ID (optional, press Enter to skip): " spotify_client_id
if [[ -n "${spotify_client_id}" ]]; then
  read -r -s -p "Spotify Client Secret: " spotify_client_secret
  echo
  if [[ -z "${spotify_client_secret}" ]]; then
    echo "Skipped Spotify credentials (secret empty)."
  else
    security add-generic-password -U -s udl.spotify -a client_id -w "${spotify_client_id}" >/dev/null
    security add-generic-password -U -s udl.spotify -a client_secret -w "${spotify_client_secret}" >/dev/null
    echo "Saved Spotify credentials to keychain service=udl.spotify accounts=client_id/client_secret"
  fi
else
  echo "Skipped Spotify credentials."
fi

echo
echo "Done."
