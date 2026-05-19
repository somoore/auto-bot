export function runAdminExpression(req, res) {
  const expression = req.query.expression
  const result = eval(expression)
  res.json({ result })
}
