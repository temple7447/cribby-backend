"""listing photos

Revision ID: a1f2c3d4e5b6
Revises: d383b8895691
Create Date: 2026-08-30 00:00:00.000000

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision: str = 'a1f2c3d4e5b6'
down_revision: Union[str, Sequence[str], None] = 'd383b8895691'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    """Upgrade schema."""
    with op.batch_alter_table('listings', schema=None) as batch_op:
        batch_op.add_column(
            sa.Column('photos', sa.JSON(), nullable=False, server_default='[]')
        )


def downgrade() -> None:
    """Downgrade schema."""
    with op.batch_alter_table('listings', schema=None) as batch_op:
        batch_op.drop_column('photos')
