for f in $(find . -name go.mod); do
  (cd $(dirname "$f"); echo "Executing go mod tidy in: $(pwd)"; go mod tidy)
done
