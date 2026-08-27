export type ApiError = {
  status: number
  code: string
  message: string
}

export function messageFor(err: ApiError): string {
  switch (err.status) {
    case 401:
      return "That API key isn't valid."
    case 500:
      return "Something went wrong on our end."
    default:
      return err.message
  }
}
