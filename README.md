# saas - Spatch As A Service

Starting from DRBD kernel module version 9.0.20 we use [spatch](http://coccinelle.lip6.fr/) to generate
compatibility patches that are required to build the kernel module on a host machine. Older kernels need
different and more patches than new ones as we try to use latest Linux upstream features as our base line in
DRBD.

While using `spatch` makes maintenance a lot easier, it slightly complicates things for users just trying to
compile one of our release-tarballs on their machine.

Our tarballs ship with a wide range of pre-generated patches, but we can never build for everybody (e.g.,
locally patched kernels, distribution kernels of distributions we don't build for in the first place,...).

Additionally, `spatch` is a beast on its own. As of now we require a very current version that is not packaged
in any distribution, and even if, we actually don't like to force a ton of OCaml dependencies on everybody
trying to build a release-tarball.

This is where `saas` comes into the picture. It is a web service, hosted by [LINBIT](https://www.linbit.com),
that takes as input a "DRBD tarball version", and a "compat.h" (which describes the capabilities of your
kernel). From that, it generates a patch with `spatch`, and returns it.

## API
For the regular user, this is handled transparently by our `Makefile`s. A manual call looks similar to this:

```
curl -d "$(cat compat.h | base64 -)" -X POST http://localhost:8080/api/v1/spatch/9.0.20-0rc2
```
