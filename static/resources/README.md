# Miner Resources

This directory contains the miner binaries that can be downloaded by clients.

## Required Files

Place the following files in this directory:

- `xmrig.exe` - XMRig CPU miner binary
- `gminer.exe` - GMiner GPU miner binary

## Access URLs

Once the files are placed here, they can be accessed via:

- XMRig: `http://[panel-url]/resources/xmrig`
- GMiner: `http://[panel-url]/resources/gminer`

These endpoints do not require authentication and can be accessed directly by miner clients.

## Notes

- The panel will serve these files with appropriate headers for binary downloads
- Make sure the files are the correct executables for the target platform
- The routes are relative to the panel binary location, so this works from any directory
