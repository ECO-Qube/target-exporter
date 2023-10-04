# Define a default target that will be executed when you type 'make'
all:

# Add your goals and their commands below
empa-build:
	docker build -t cristianohelio/target-exporter:empa --platform=linux/amd64 . && docker push cristianohelio/target-exporter:empa

# Define the 'all' target to depend on all your goals
all: empa-build

# This phony target ensures that 'all' is always executed,
# even if a file with the same name exists in the directory
.PHONY: all empa-build
