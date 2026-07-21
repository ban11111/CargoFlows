export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly code = "request_failed",
    public readonly requestId = "",
    public readonly details?: unknown,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

interface APIErrorPayload {
  code?: unknown;
  message?: unknown;
  request_id?: unknown;
}

export function authenticatedMediaURL(value: string): string {
  const apiPrefix = "/api/v1";
  if (!value.startsWith(apiPrefix + "/")) return value;
  return `/api/proxy${value.slice(apiPrefix.length)}`;
}

export async function apiRequest<TResponse>(
  path: string,
  options: RequestInit = {},
): Promise<TResponse> {
  const isFormData = typeof FormData !== "undefined" && options.body instanceof FormData;
  const response = await fetch(`/api/proxy${path}`, {
    ...options,
    headers: {
      ...(isFormData ? {} : { "content-type": "application/json" }),
      ...options.headers,
    },
  });

  if (!response.ok) {
    let payload: APIErrorPayload = {};
    try {
      payload = await response.json() as APIErrorPayload;
    } catch {
      // Never surface an unstructured upstream body. It may contain provider details.
    }
    throw new ApiError(
      typeof payload.message === "string" && payload.message.trim()
        ? payload.message
        : "Request failed",
      response.status,
      typeof payload.code === "string" ? payload.code : "request_failed",
      typeof payload.request_id === "string" ? payload.request_id : response.headers.get("x-request-id") ?? "",
      payload,
    );
  }

  if (response.status === 204) {
    return undefined as TResponse;
  }

  return response.json() as Promise<TResponse>;
}
