export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
  ) {
    super(message);
  }
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
  const response = await fetch(`/api/proxy${path}`, {
    ...options,
    headers: {
      "content-type": "application/json",
      ...options.headers,
    },
  });

  if (!response.ok) {
    throw new ApiError(await response.text(), response.status);
  }

  if (response.status === 204) {
    return undefined as TResponse;
  }

  return response.json() as Promise<TResponse>;
}
