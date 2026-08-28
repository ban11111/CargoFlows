const requiredSignatureParameters = [
  "X-Amz-Algorithm",
  "X-Amz-Credential",
  "X-Amz-Date",
  "X-Amz-Expires",
  "X-Amz-SignedHeaders",
  "X-Amz-Signature",
] as const;

export function hasPresignedUploadSignature(url: URL): boolean {
  const expires = Number(url.searchParams.get("X-Amz-Expires"));
  return (
    requiredSignatureParameters.every((parameter) => url.searchParams.has(parameter)) &&
    Number.isInteger(expires) &&
    expires > 0 &&
    expires <= 900
  );
}

export function publicRequestOrigin(requestURL: URL, headers: Headers): string {
  const host = headers.get("x-forwarded-host") ?? headers.get("host") ?? requestURL.host;
  const protocol = headers.get("x-forwarded-proto") ?? requestURL.protocol.replace(":", "");
  return `${protocol}://${host}`;
}

export function proxyUploadURL(rawURL: string, origin: string, bucket: string, upstreamEndpoint: string): string {
	let uploadURL: URL;
	let trustedUpstream: URL;
	try {
		uploadURL = new URL(rawURL);
		trustedUpstream = new URL(upstreamEndpoint);
	} catch {
		return rawURL;
	}

	if (
		trustedUpstream.protocol !== "http:" ||
		trustedUpstream.username !== "" ||
		trustedUpstream.password !== "" ||
		trustedUpstream.pathname !== "/" ||
		trustedUpstream.search !== "" ||
		trustedUpstream.hash !== "" ||
		uploadURL.origin !== trustedUpstream.origin ||
		!uploadURL.pathname.startsWith(`/${bucket}/`) ||
		!hasPresignedUploadSignature(uploadURL)
  ) {
    return rawURL;
  }

  const publicURL = new URL(`/api/storage${uploadURL.pathname}`, origin);
  publicURL.search = uploadURL.search;
  return publicURL.toString();
}

export function storageUpstreamURL(
  path: string[],
  searchParams: URLSearchParams,
  endpoint: string,
  bucket: string,
): URL | null {
	if (path[0] !== bucket || !path[1]) return null;
	let configured: URL;
	try {
		configured = new URL(endpoint);
	} catch {
		return null;
	}
	if (
		configured.protocol !== "http:" ||
		configured.username !== "" ||
		configured.password !== "" ||
		configured.pathname !== "/" ||
		configured.search !== "" ||
		configured.hash !== ""
	) return null;

	const upstream = new URL(path.map(encodeURIComponent).join("/"), configured);
  searchParams.forEach((value, key) => upstream.searchParams.append(key, value));
  return hasPresignedUploadSignature(upstream) ? upstream : null;
}
