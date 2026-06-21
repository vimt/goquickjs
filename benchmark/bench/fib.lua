-- iterative fibonacci (mod P), repeated __N__ times. Tests tight scalar loops.
local function main()
  local reps = __N__
  local total = 0
  for r = 0, reps - 1 do
    local a, b = 0, 1
    for i = 0, 89 do
      local t = (a + b) % 1000000007
      a = b
      b = t
    end
    total = (total + a) % 1000000007
  end
  return total
end
result = string.format("%d", main())
