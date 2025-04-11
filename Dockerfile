# --------------------------------------------
# ✅ Optional build-time metadata for the final image
# --------------------------------------------
	ARG COMMIT=""
	ARG VERSION=""
	ARG BUILDNUM=""
	
	# --------------------------------------------
	# ✅ Stage 1: Build WorldLand binaries in a Go builder container
	# --------------------------------------------
	FROM golang:1.24-alpine AS builder
	
	# Install required build tools
	RUN apk add --no-cache gcc musl-dev linux-headers git
	
	# Copy Go module files and download dependencies
	COPY go.mod /worldland/
	COPY go.sum /worldland/
	RUN cd /worldland && go mod download
	
	# Copy the full project source
	ADD . /worldland
	
	# Build the WorldLand binary (statically linked)
	RUN cd /worldland && go run build/ci.go install -static ./cmd/worldland
	
	# --------------------------------------------
	# ✅ Stage 2: Create a minimal runtime container with the built binary
	# --------------------------------------------
	FROM alpine:latest
	
	# Install runtime dependencies
	RUN apk add --no-cache ca-certificates
	
	# Copy the compiled worldland binary
	COPY --from=builder /worldland/build/bin/worldland /usr/local/bin/
	
	# Expose standard blockchain ports
	EXPOSE 8545 8546 30303 30303/udp
	
	# Default entrypoint for the container
	ENTRYPOINT ["worldland"]
	
	# --------------------------------------------
	# ✅ Metadata labels for programmatic image tracking
	# --------------------------------------------
	ARG COMMIT=""
	ARG VERSION=""
	ARG BUILDNUM=""
	
	LABEL commit="$COMMIT" version="$VERSION" buildnum="$BUILDNUM"
	