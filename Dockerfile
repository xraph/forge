FROM scratch

# Copy the binary from the build context
COPY forge /usr/local/bin/forge

# Set the entrypoint
ENTRYPOINT ["/usr/local/bin/forge"]

# Add labels for better container metadata
LABEL org.opencontainers.image.title="Forge"
LABEL org.opencontainers.image.description="Comprehensive backend framework for Go with enterprise-grade features"
LABEL org.opencontainers.image.vendor="forge-framework"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.documentation="https://github.com/forge-framework/forge/blob/main/README.md"
LABEL org.opencontainers.image.url="https://github.com/forge-framework/forge"