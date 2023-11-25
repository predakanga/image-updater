# image-updater

A simple webhook for updating image tags in kustomization files.

This program is inspired by services like [RenovateBot](https://github.com/renovatebot/renovate) and [Argo CD Image Updater](https://github.com/argoproj-labs/argocd-image-updater), but with a significantly reduced scope.

Specifically, this service is designed to:
- Be triggered manually
- Allow the triggerer to specify the new image tag
- Update resource files, not kubernetes resources
- Create minimal git diffs (no whitespace changes/reordering)

At the minute it only handles kustomization files, but Helm values files and more are planned.