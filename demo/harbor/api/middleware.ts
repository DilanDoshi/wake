import type { Request, Response, NextFunction } from "./http"

export type Middleware = (req: Request, res: Response, next: NextFunction) => void

/** Applies each middleware in order, outermost first. */
export function chain(...mw: Middleware[]): Middleware {
  return (req, res, next) =>
    mw.reduceRight<NextFunction>((acc, m) => () => m(req, res, acc), next)()
}

/** Resolves the API key on the request and puts it on the context. */
export const withAuth: Middleware = (req, res, next) => {
  const key = req.header("x-harbor-key")
  if (!key) {
    res.status(401).json({ code: "missing_key", message: "missing api key" })
    return
  }
  req.ctx.key = key
  next()
}
