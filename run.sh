#!/bin/bash
set -e

# Start bash if requested
if [ "$1" = 'bash' ]; then
    exec "$@"
else
    exec /file-downloader-sidecar --namespace $NAMESPACE --config-map $CONFIG_MAP --download-path $PLUGIN_PATH
fi