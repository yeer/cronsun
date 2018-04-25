#!/bin/sh
BASEDIR=$(dirname $(realpath $0))
cd $BASEDIR/ui
npm run build
cd ..
go-bindata -pkg "web" -prefix "ui/dist/" -o static_assets.go ./ui/dist/

VER=$(git rev-parse --short HEAD)
echo $BASEDIR/ui/index.html
sed -i -E "s/(build\.js\?v=).{7}/\1${VER}/g" $BASEDIR/ui/index.html
