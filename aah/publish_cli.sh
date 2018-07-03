#!/usr/bin/env bash

# Creator: Jeevanandam M. (https://github.com/jeevatkm) jeeva@myjeeva.com
# License: MIT

# currently this script focused on mac os

BASEDIR=$(dirname "$0")
cd $BASEDIR

# Inputs
version=$1

# Build for macOS
build_dir="/tmp/aah_cli_mac_$version"
mkdir -p $build_dir
env GOOS=darwin GOARCH=amd64 go build -o $build_dir/aah -ldflags="-s -w"
cd $build_dir && zip aah_darwin_amd64.zip aah

# sha256 and upload to aah server
sha256=$(/usr/bin/shasum -a 256 $build_dir/aah_darwin_amd64.zip | cut -d " " -f 1)
ssh root@aahframework.org "mkdir -p /srv/www/aahframework.org/public/releases/cli/$version"
scp $build_dir/aah_darwin_amd64.zip root@aahframework.org:/srv/www/aahframework.org/public/releases/cli/$version

# update homebrew tap
if [ ! -d "$GOPATH/src/github.com/go-aah/homebrew-tap" ]; then
  git clone git@github.com:go-aah/homebrew-tap.git $GOPATH/src/github.com/go-aah/homebrew-tap
fi
cd $GOPATH/src/github.com/go-aah/homebrew-tap
sed -i '' -e 's/cli\/.*\/aah_darwin_amd64.zip/cli\/'"$version"'\/aah_darwin_amd64.zip/g' ./Formula/aah.rb
sed -i '' -e 's/sha256 ".*"/sha256 "'"$sha256"'"/g' ./Formula/aah.rb
sed -i '' -e 's/version ".*"/version "'"$version"'"/g' ./Formula/aah.rb 
git add -u && git commit -m "brew tap update with $version release" && git push

# Cleanup
rm -rf $build_dir

# .. next upcoming OS support