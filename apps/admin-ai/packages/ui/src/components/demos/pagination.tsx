import { useState } from "react"
import { useTranslations } from "use-intl"

import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "../pagination"
import { DemoSection } from "./_section"

export function PaginationDemo() {
  const t = useTranslations("showcase.demos.pagination")
  const [page, setPage] = useState(2)

  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <Pagination>
          <PaginationContent>
            <PaginationItem>
              <PaginationPrevious href="#" />
            </PaginationItem>
            <PaginationItem>
              <PaginationLink
                href="#"
                isActive={page === 1}
                onClick={(e) => {
                  e.preventDefault()
                  setPage(1)
                }}
              >
                1
              </PaginationLink>
            </PaginationItem>
            <PaginationItem>
              <PaginationLink
                href="#"
                isActive={page === 2}
                onClick={(e) => {
                  e.preventDefault()
                  setPage(2)
                }}
              >
                2
              </PaginationLink>
            </PaginationItem>
            <PaginationItem>
              <PaginationLink
                href="#"
                isActive={page === 3}
                onClick={(e) => {
                  e.preventDefault()
                  setPage(3)
                }}
              >
                3
              </PaginationLink>
            </PaginationItem>
            <PaginationItem>
              <PaginationNext href="#" />
            </PaginationItem>
          </PaginationContent>
        </Pagination>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("currentPage", { page })}
        </p>
      </DemoSection>

      <DemoSection titleKey="withIcons">
        <Pagination>
          <PaginationContent>
            <PaginationItem>
              <PaginationPrevious href="#" />
            </PaginationItem>
            <PaginationItem>
              <PaginationLink href="#" isActive>
                1
              </PaginationLink>
            </PaginationItem>
            <PaginationItem>
              <PaginationLink href="#">2</PaginationLink>
            </PaginationItem>
            <PaginationItem>
              <PaginationEllipsis />
            </PaginationItem>
            <PaginationItem>
              <PaginationLink href="#">9</PaginationLink>
            </PaginationItem>
            <PaginationItem>
              <PaginationLink href="#">10</PaginationLink>
            </PaginationItem>
            <PaginationItem>
              <PaginationNext href="#" />
            </PaginationItem>
          </PaginationContent>
        </Pagination>
      </DemoSection>
    </div>
  )
}
