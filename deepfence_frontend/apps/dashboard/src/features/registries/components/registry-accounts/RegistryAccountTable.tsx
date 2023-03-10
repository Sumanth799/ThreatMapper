import { useMemo } from 'react';
import { generatePath, useParams } from 'react-router-dom';
import { createColumnHelper, Table } from 'ui-components';

import { ModelRegistryListResp } from '@/api/generated';
import { DFLink } from '@/components/DFLink';

export const RegistryAccountTable = ({ data }: { data: ModelRegistryListResp[] }) => {
  const { account } = useParams() as {
    account: string;
  };

  const columnHelper = createColumnHelper<ModelRegistryListResp>();
  const columns = useMemo(
    () => [
      columnHelper.accessor('name', {
        header: () => 'Name',
        cell: (info) => (
          <div>
            <DFLink
              to={generatePath('/registries/images/:account/:accountId', {
                account,
                accountId: info.row.original.id?.toString() ?? '',
              })}
            >
              {info.renderValue()}
            </DFLink>
          </div>
        ),
        minSize: 150,
      }),
      columnHelper.accessor('created_at', {
        header: () => 'Created',
        minSize: 150,
      }),
      columnHelper.accessor('non_secret', {
        header: () => 'Credentials',
        cell: (info) => <div>{JSON.stringify(info.renderValue())}</div>,
        minSize: 150,
      }),
    ],
    [],
  );
  return <Table columns={columns} data={data} />;
};
